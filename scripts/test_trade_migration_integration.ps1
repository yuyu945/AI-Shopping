[CmdletBinding()]
param(
    [ValidateRange(1025, 65535)]
    [int]$MySQLPort = 33306,
    [switch]$KeepEnvironment
)

$ErrorActionPreference = 'Stop'
if ($env:AI_SHOPPING_TRADE_MIGRATION_INTEGRATION -ne '1') {
    Write-Output 'SKIP: set AI_SHOPPING_TRADE_MIGRATION_INTEGRATION=1 to run the isolated trade migration integration test.'
    exit 0
}
if ([string]::IsNullOrWhiteSpace($env:MYSQL_PASSWORD) -or [string]::IsNullOrWhiteSpace($env:MYSQL_ROOT_PASSWORD)) {
    throw 'MYSQL_PASSWORD and MYSQL_ROOT_PASSWORD must be set for the isolated integration environment.'
}

$runID = [guid]::NewGuid().ToString('N')
$project = "m21trade$runID"
$hadProject = Test-Path Env:COMPOSE_PROJECT_NAME
$previousProject = $env:COMPOSE_PROJECT_NAME
$hadPort = Test-Path Env:MYSQL_PORT
$previousPort = $env:MYSQL_PORT
$hadMigrationDsn = Test-Path Env:AI_SHOPPING_MYSQL_DSN
$previousMigrationDsn = $env:AI_SHOPPING_MYSQL_DSN
$env:COMPOSE_PROJECT_NAME = $project
$env:MYSQL_PORT = $MySQLPort.ToString()
$composeFile = 'deploy/docker-compose.yml'
$container = "$project-mysql-1"
$legacyFixture = Join-Path $PSScriptRoot '..\deploy\mysql\fixtures\legacy-precheck-trade-schema.sql'
$primaryFailure = $null
$cleanupFailure = $null

function Invoke-RootSQL {
    param([Parameter(Mandatory)][string]$Sql)
    $output = @($Sql | docker compose -f $composeFile exec -T mysql sh -c 'exec mysql -uroot --password="$MYSQL_ROOT_PASSWORD" --database=trade_db --batch --skip-column-names' 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Isolated MySQL assertion query failed.' }
    return @($output | Where-Object { $_ -notmatch '^mysql: \[Warning\] Using a password on the command line interface can be insecure\.$' })
}

try {
    docker compose -f $composeFile up -d mysql | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not start isolated MySQL Compose service.' }
    $deadline = (Get-Date).AddSeconds(90)
    do {
        $health = (docker inspect --format '{{.State.Health.Status}}' $container 2>$null).Trim()
        if ($health -eq 'healthy') { break }
        if ((Get-Date) -ge $deadline) { throw 'Isolated MySQL did not become healthy within 90 seconds.' }
        Start-Sleep -Seconds 2
    } while ($true)

    function Apply-AndAssert([string]$caseName) {
        & "$PSScriptRoot\apply_migrations.ps1" -ComposeFile $composeFile
        & "$PSScriptRoot\apply_migrations.ps1" -ComposeFile $composeFile
        $result = @(Invoke-RootSQL -Sql "SELECT COUNT(*) FROM schema_migrations WHERE version IN ('20260822_m2_1_trade_schema','20260822_m2_1_trade_z_order_promotion_candidates'); SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'trade_db' AND table_name IN ('carts','cart_items','orders','order_items'); SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'FOREIGN KEY'; SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND numeric_precision = 12 AND numeric_scale = 2; SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'CHECK'; SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name = 'order_items' AND column_name = 'candidate_promotions_snapshot' AND is_nullable = 'NO';" | ? { $_ -match '^\d+$' })
        if (($result -join ',') -ne '2,4,2,5,2,1') { throw "$caseName migration assertions failed: $($result -join ',')." }
    }
    Invoke-RootSQL -Sql 'DROP TABLE IF EXISTS order_items; DROP TABLE IF EXISTS cart_items; DROP TABLE IF EXISTS orders; DROP TABLE IF EXISTS carts; DROP TABLE IF EXISTS schema_migrations;' | Out-Null
    $dsn = "app:$($env:MYSQL_PASSWORD)@tcp(127.0.0.1:$MySQLPort)/trade_db"
    $env:AI_SHOPPING_MYSQL_DSN = $dsn
    Apply-AndAssert 'empty trade_db'
    Invoke-RootSQL -Sql 'DROP TABLE IF EXISTS order_items; DROP TABLE IF EXISTS cart_items; DROP TABLE IF EXISTS orders; DROP TABLE IF EXISTS carts; DROP TABLE IF EXISTS schema_migrations;' | Out-Null
    Invoke-RootSQL -Sql (Get-Content -LiteralPath $legacyFixture -Raw) | Out-Null
    Apply-AndAssert 'legacy pre-check schema'
    $legacyChecks = @(Invoke-RootSQL -Sql "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='trade_db' AND ((table_name='cart_items' AND column_name IN ('created_at','updated_at')) OR (table_name='orders' AND column_name IN ('created_at','updated_at','paid_at','closed_at','shipping_name_snapshot','shipping_phone_snapshot','shipping_address_snapshot')) OR (table_name='order_items' AND column_name IN ('created_at','product_title_snapshot','sku_spec_snapshot','promotion_snapshot','candidate_promotions_snapshot'))); SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='trade_db' AND ((table_name='orders' AND column_name IN ('total_amount','paid_amount')) OR (table_name='order_items' AND column_name IN ('unit_price','discount_amount','item_amount'))) AND column_type='decimal(12,2)'; SELECT MAX(non_unique) FROM information_schema.statistics WHERE table_schema='trade_db' AND table_name='orders' AND index_name='uq_orders_user_request'; SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema='trade_db' AND table_name='orders' AND index_name='uq_orders_user_request'; SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index) FROM information_schema.statistics WHERE table_schema='trade_db' AND table_name='orders' AND index_name='idx_orders_user_created'; SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics WHERE table_schema='trade_db' AND table_name='orders' AND index_name IN ('uq_orders_order_no','uq_orders_user_request','idx_orders_user_created');" | ? { -not [string]::IsNullOrWhiteSpace($_) })
    if (($legacyChecks -join ',') -ne '14,5,0,user_id,request_id,user_id,created_at,3') { throw "Legacy preservation assertions failed: $($legacyChecks -join ',')." }

    $result = @(Invoke-RootSQL -Sql "SELECT COUNT(*) FROM schema_migrations WHERE version IN ('20260822_m2_1_trade_schema','20260822_m2_1_trade_z_order_promotion_candidates'); SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'trade_db' AND table_name IN ('carts','cart_items','orders','order_items'); SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'FOREIGN KEY'; SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND numeric_precision = 12 AND numeric_scale = 2; SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'CHECK'; SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name IN ('carts','cart_items','orders','order_items') AND column_name = 'created_at'; SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics WHERE table_schema = 'trade_db' AND table_name = 'orders' AND index_name = 'idx_orders_user_created'; SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND table_name = 'order_items' AND column_name = 'candidate_promotions_snapshot' AND is_nullable = 'NO';" | Where-Object { $_ -match '^\d+$' })
    if (($result -join ',') -ne '2,4,2,5,2,4,1,1') { throw "Unexpected trade migration assertion counts: $($result -join ',')." }
    Write-Output "Verified isolated trade migration run $runID."
}
catch {
    $primaryFailure = $_
}
finally {
    if (-not $KeepEnvironment) {
        try {
            docker compose -f $composeFile down -v --remove-orphans | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Isolated MySQL Compose cleanup failed.' }
        }
        catch {
            $cleanupFailure = $_
        }
    }
    if ($hadProject) {
        $env:COMPOSE_PROJECT_NAME = $previousProject
    }
    else {
        Remove-Item Env:COMPOSE_PROJECT_NAME -ErrorAction SilentlyContinue
    }
    if ($hadPort) {
        $env:MYSQL_PORT = $previousPort
    }
    else {
        Remove-Item Env:MYSQL_PORT -ErrorAction SilentlyContinue
    }
    if ($hadMigrationDsn) {
        $env:AI_SHOPPING_MYSQL_DSN = $previousMigrationDsn
    }
    else {
        Remove-Item Env:AI_SHOPPING_MYSQL_DSN -ErrorAction SilentlyContinue
    }
}
if ($null -ne $primaryFailure) {
    throw $primaryFailure
}
if ($null -ne $cleanupFailure) {
    throw 'Isolated MySQL Compose cleanup failed.'
}
