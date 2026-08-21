[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$RunID,
    [string]$Container = 'm12cacheverify-mysql-1',
    [string]$SeedFile = 'deploy/mysql/seed/02-catalog-seed.sql'
)

$ErrorActionPreference = 'Stop'

if ($Container -ne 'm12cacheverify-mysql-1') {
    throw 'Cache invalidation integration preparation only supports the m12cacheverify MySQL container.'
}
if ([string]::IsNullOrWhiteSpace($env:MYSQL_ROOT_PASSWORD)) {
    throw 'MYSQL_ROOT_PASSWORD must be set in the process environment.'
}

$parsedRunID = [guid]::Empty
if (-not [guid]::TryParse($RunID, [ref]$parsedRunID)) {
    throw 'RunID must be a UUID.'
}

$containerStatus = docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' $Container
if ($LASTEXITCODE -ne 0 -or $containerStatus -ne 'running healthy') {
    throw "Container $Container must be running and healthy."
}

& "$PSScriptRoot\seed_catalog.ps1" -Container $Container -SeedFile $SeedFile
if ($LASTEXITCODE -ne 0) {
    throw 'Catalog seed failed.'
}

$normalizedRunID = $parsedRunID.ToString()
$guardSQL = @"
CREATE TABLE IF NOT EXISTS catalog_db.cache_invalidation_integration_guards (
    run_id CHAR(36) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (run_id)
) ENGINE=InnoDB;
INSERT INTO catalog_db.cache_invalidation_integration_guards (run_id)
VALUES ('$normalizedRunID')
ON DUPLICATE KEY UPDATE created_at = VALUES(created_at);
"@

docker exec $Container sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -e "$1"' -- $guardSQL
if ($LASTEXITCODE -ne 0) {
    throw 'Database integration guard creation failed.'
}

Write-Output "Cache invalidation integration database guard prepared for run ID $normalizedRunID."
