#!/bin/sh
set -e

echo "Migration entrypoint starting..."

until pg_isready -h db -p 5432 -U "$POSTGRES_USER"; do
  echo "Waiting for postgres..."
  sleep 1
done

# Ensure the target database exists (create if missing)
exists=$(PGPASSWORD="$POSTGRES_PASSWORD" psql -h db -U "$POSTGRES_USER" -tAc "SELECT 1 FROM pg_database WHERE datname='$POSTGRES_DB'" )
if [ "$exists" != "1" ]; then
  echo "Database $POSTGRES_DB does not exist — creating"
  PGPASSWORD="$POSTGRES_PASSWORD" psql -h db -U "$POSTGRES_USER" -c "CREATE DATABASE \"$POSTGRES_DB\";"
fi

# Directory for storing backups (can be overridden by setting BACKUP_DIR env var)
BACKUP_DIR="${BACKUP_DIR:-/backups}"
mkdir -p "$BACKUP_DIR"

# If the database already exists, take a pre-migration backup so we can restore on failure
BACKUP_FILE=""
if [ "$exists" = "1" ]; then
  BACKUP_FILE="$BACKUP_DIR/pre_migration_$(date +%Y%m%d%H%M%S).sql"
  echo "Database $POSTGRES_DB exists — creating pre-migration backup: $BACKUP_FILE"
  PGPASSWORD="$POSTGRES_PASSWORD" pg_dump -h db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fp -f "$BACKUP_FILE"
fi

# On any error during migrations, restore the pre-migration backup (if present)
restore_on_error() {
  rc=$1
  if [ -n "$BACKUP_FILE" ] && [ -f "$BACKUP_FILE" ]; then
    echo "Migration failed (rc=$rc) — restoring database from $BACKUP_FILE"
    # Try restoring the plain SQL dump
    # Drop and recreate the public schema to ensure the restore applies cleanly
    PGPASSWORD="$POSTGRES_PASSWORD" psql -h db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
    PGPASSWORD="$POSTGRES_PASSWORD" psql -h db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f "$BACKUP_FILE" || {
      echo "Restore failed. Exiting with original error code $rc"
      exit 1
    }
    echo "Restore complete. Exiting with original error code $rc"
  fi
  exit $rc
}

# Use EXIT trap (portable) to detect any failure and restore from pre-migration backup.
# POSIX /bin/sh does not support the "ERR" trap on some systems, so trap on EXIT
# and check the exit status.
trap 'rc=$?; if [ "$rc" -ne 0 ]; then restore_on_error $rc; fi' EXIT

for f in /migrations/*.sql; do
  echo "Applying $f"
  PGPASSWORD="$POSTGRES_PASSWORD" psql -h db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f "$f"
done

# If we completed migrations successfully, remove the EXIT trap
trap - EXIT

echo "Migrations complete."

echo "Migrations complete."
