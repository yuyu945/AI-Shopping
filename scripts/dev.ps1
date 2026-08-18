[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$Action
)

$usage = 'Usage: .\scripts\dev.ps1 <test|vet|fmt|deps-up|deps-down>'

if ([string]::IsNullOrWhiteSpace($Action)) {
    Write-Error $usage
    exit 1
}

switch ($Action) {
    'test' { go test ./... }
    'vet' { go vet ./... }
    'fmt' { go fmt ./... }
    'deps-up' { docker compose -f deploy/docker-compose.yml up -d }
    'deps-down' { docker compose -f deploy/docker-compose.yml down }
    default {
        Write-Error $usage
        exit 1
    }
}

exit $LASTEXITCODE
