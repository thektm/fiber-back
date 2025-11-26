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

for f in /migrations/*.sql; do
  echo "Applying $f"
  PGPASSWORD="$POSTGRES_PASSWORD" psql -h db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f "$f"
done

echo "Migrations complete."
