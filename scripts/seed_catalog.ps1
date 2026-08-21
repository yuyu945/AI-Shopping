[CmdletBinding()]
param(
    [string]$Container = 'deploy-mysql-1',
    [string]$SeedFile = 'deploy/mysql/seed/02-catalog-seed.sql'
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($env:MYSQL_ROOT_PASSWORD)) {
    throw 'MYSQL_ROOT_PASSWORD must be set in the process environment.'
}
if (-not (Test-Path -LiteralPath $SeedFile)) {
    throw "Seed file not found: $SeedFile"
}

Write-Output "Applying catalog seed to $Container..."
Get-Content -LiteralPath $SeedFile -Raw |
    docker exec -i $Container sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot'
if ($LASTEXITCODE -ne 0) {
    throw 'Catalog seed failed.'
}

$counts = docker exec $Container sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -N -e "$1"' -- "SELECT CONCAT('products=', COUNT(*)) FROM catalog_db.products; SELECT CONCAT('skus=', COUNT(*)) FROM catalog_db.product_skus; SELECT CONCAT('promotions=', COUNT(*)) FROM catalog_db.promotion_rules;"
if ($LASTEXITCODE -ne 0) {
    throw 'Catalog seed verification failed.'
}
Write-Output 'Catalog seed applied successfully.'
$counts | ForEach-Object { Write-Output $_ }
