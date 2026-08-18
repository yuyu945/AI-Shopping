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
    'test' {
        $packages = @(go list ./...)
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        if ($packages.Count -eq 0) {
            Write-Output 'No Go packages to test.'
            exit 0
        }
        go test $packages
    }
    'vet' {
        $packages = @(go list ./...)
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        if ($packages.Count -eq 0) {
            Write-Output 'No Go packages to vet.'
            exit 0
        }
        go vet $packages
    }
    'fmt' { go fmt ./... }
    'deps-up' { docker compose -f deploy/docker-compose.yml up -d }
    'deps-down' { docker compose -f deploy/docker-compose.yml down }
    default {
        Write-Error $usage
        exit 1
    }
}

exit $LASTEXITCODE
