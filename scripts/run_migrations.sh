#!/usr/bin/env sh
echo "Running DB migrations via docker compose migrate service..."
docker compose run --rm migrate
rc=$?
if [ $rc -ne 0 ]; then
  echo "Migrations failed with exit code $rc" >&2
  exit $rc
fi
echo "Migrations applied successfully."
