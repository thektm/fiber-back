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
# Run migrate service (idempotent). Continue even if migrate errors to allow inspection.
docker compose run --rm migrate || true

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
