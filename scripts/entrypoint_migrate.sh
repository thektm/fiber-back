#!/bin/sh
set -e

echo "Migration entrypoint starting..."

until pg_isready -h db -p 5432 -U "$POSTGRES_USER"; do
  echo "Waiting for postgres..."
  sleep 1
done

for f in /migrations/*.sql; do
  echo "Applying $f"
  PGPASSWORD="$POSTGRES_PASSWORD" psql -h db -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f "$f"
done

echo "Migrations complete."
