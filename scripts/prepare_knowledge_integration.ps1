[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$RunID,
    [string]$Container = 'm32knowledge-mysql-1'
)

$ErrorActionPreference = 'Stop'

if ($env:COMPOSE_PROJECT_NAME -ne 'm32knowledge') {
    throw 'Knowledge integration preparation only supports COMPOSE_PROJECT_NAME=m32knowledge.'
}
if ($Container -ne 'm32knowledge-mysql-1') {
    throw 'Knowledge integration preparation only supports the m32knowledge MySQL container.'
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

$normalizedRunID = $parsedRunID.ToString()
$guardSQL = @"
CREATE TABLE IF NOT EXISTS knowledge_db.knowledge_integration_guards (
    run_id CHAR(36) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (run_id)
) ENGINE=InnoDB;
INSERT INTO knowledge_db.knowledge_integration_guards (run_id)
VALUES ('$normalizedRunID')
ON DUPLICATE KEY UPDATE created_at = VALUES(created_at);
"@

docker exec $Container sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -e "$1"' -- $guardSQL
if ($LASTEXITCODE -ne 0) {
    throw 'Knowledge integration database guard creation failed.'
}

Write-Output "Knowledge integration database guard prepared for run ID $normalizedRunID."
