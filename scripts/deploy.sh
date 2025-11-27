#!/usr/bin/env bash
set -euo pipefail

# Simple deploy script for server
# Usage: ./scripts/deploy.sh [branch]
# - fetches and checks out branch
# - builds docker images
# - runs migrations
# - brings up the compose stack

BRANCH=${1:-main}
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Take a pre-deploy backup of the database so we can restore if deploy/migrations break.
# Default `BACKUP_DIR` can be overridden by environment; host dir will be mounted into the
# container used to run `pg_dump` so backups persist on the host.
BACKUP_DIR="${BACKUP_DIR:-$REPO_ROOT/backups}"
mkdir -p "$BACKUP_DIR"

TIMESTAMP="$(date +%Y%m%d%H%M%S)"
BACKUP_FILE="$BACKUP_DIR/pre_deploy_${TIMESTAMP}.sql"
BACKUP_BASENAME="$(basename "$BACKUP_FILE")"

echo "Creating pre-deploy backup: $BACKUP_FILE"
# Run pg_dump inside the `migrate` container and write to the mounted backups dir.
if ! docker compose run --rm -v "$BACKUP_DIR:/backups" -e BACKUP_NAME="$BACKUP_BASENAME" --entrypoint sh migrate -c 'PGPASSWORD="$POSTGRES_PASSWORD" pg_dump -h db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fp -f /backups/"$BACKUP_NAME"'; then
  echo "Pre-deploy backup failed — aborting deploy"
  exit 2
fi

echo "Pre-deploy backup saved to $BACKUP_FILE"

LOCKFILE=/tmp/deploy.lock
if [ -e "$LOCKFILE" ]; then
  echo "Another deploy appears to be running. Exiting."
  exit 1
fi
touch "$LOCKFILE"
trap 'rm -f "$LOCKFILE"' EXIT

echo "Deploying branch: $BRANCH"

echo "Fetching latest from origin..."
git fetch origin --prune
git checkout "$BRANCH"
git reset --hard "origin/$BRANCH"

echo "Building images (pulling base images if available)..."
# Try to pull remote images first; ignore errors if none
docker compose pull || true

docker compose build --pull --parallel

echo "Running migrations..."
# Run migrate service (idempotent). If migrations fail, abort the deploy so we don't
# start the app against a partially-migrated or inconsistent database. Previously
# we continued and attempted an automatic restore which produced duplicate-key
# failures; failing fast makes the problem visible for manual remediation.
if ! docker compose run --rm -e AUTO_RESTORE_ON_FAIL=0 migrate; then
  echo "Migrations failed — aborting deploy. Inspect migrate container logs."
  exit 1
fi

echo "Starting services..."
docker compose up -d --remove-orphans

echo "Waiting for app health..."
HEALTH_URL="http://127.0.0.1:3001/health"
MAX_RETRIES=15
SLEEP=2
i=0
while [ $i -lt $MAX_RETRIES ]; do
  if curl -sSf "$HEALTH_URL" >/dev/null 2>&1; then
    echo "App is healthy"
    exit 0
  fi
  echo "Waiting for app... ($i/$MAX_RETRIES)"
  i=$((i+1))
  sleep $SLEEP
done

echo "Health check failed after $((MAX_RETRIES * SLEEP)) seconds"
exit 2
