#!/usr/bin/env pwsh
Write-Host "Running DB migrations via docker compose migrate service..."
docker compose run --rm migrate
if ($LASTEXITCODE -ne 0) {
    Write-Error "Migrations failed with exit code $LASTEXITCODE"
    exit $LASTEXITCODE
}
Write-Host "Migrations applied successfully."
