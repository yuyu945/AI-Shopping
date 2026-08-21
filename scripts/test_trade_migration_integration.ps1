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
$previousProject = $env:COMPOSE_PROJECT_NAME
$previousPort = $env:MYSQL_PORT
$env:COMPOSE_PROJECT_NAME = $project
$env:MYSQL_PORT = $MySQLPort.ToString()
$composeFile = 'deploy/docker-compose.yml'
$container = "$project-mysql-1"

function Invoke-RootSQL {
    param([Parameter(Mandatory)][string]$Sql)
    $output = @($Sql | docker compose -f $composeFile exec -T -e "MYSQL_PWD=$($env:MYSQL_ROOT_PASSWORD)" mysql mysql -uroot --database=trade_db --batch --skip-column-names 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Isolated MySQL assertion query failed.' }
    return $output
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

    Invoke-RootSQL -Sql 'DROP TABLE IF EXISTS order_items; DROP TABLE IF EXISTS cart_items; DROP TABLE IF EXISTS orders; DROP TABLE IF EXISTS carts; DROP TABLE IF EXISTS schema_migrations; CREATE TABLE carts (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, user_id BIGINT UNSIGNED NOT NULL, PRIMARY KEY (id), UNIQUE KEY uq_carts_user (user_id)) ENGINE=InnoDB; CREATE TABLE cart_items (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, cart_id BIGINT UNSIGNED NOT NULL, sku_id BIGINT UNSIGNED NOT NULL, quantity INT UNSIGNED NOT NULL, selected BOOLEAN NOT NULL DEFAULT TRUE, PRIMARY KEY (id), UNIQUE KEY uq_cart_items_cart_sku (cart_id, sku_id), KEY idx_cart_items_cart (cart_id), CONSTRAINT fk_cart_items_cart FOREIGN KEY (cart_id) REFERENCES carts(id)) ENGINE=InnoDB; CREATE TABLE orders (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, order_no VARCHAR(64) NOT NULL, user_id BIGINT UNSIGNED NOT NULL, request_id VARCHAR(128) NOT NULL, status VARCHAR(32) NOT NULL, total_amount DECIMAL(12,2) NOT NULL, paid_amount DECIMAL(12,2) NOT NULL DEFAULT 0.00, shipping_name_snapshot VARCHAR(128) NOT NULL, shipping_phone_snapshot VARCHAR(32) NOT NULL, shipping_address_snapshot JSON NOT NULL, PRIMARY KEY (id), UNIQUE KEY uq_orders_order_no (order_no), UNIQUE KEY uq_orders_user_request (user_id, request_id)) ENGINE=InnoDB; CREATE TABLE order_items (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, order_id BIGINT UNSIGNED NOT NULL, product_id BIGINT UNSIGNED NOT NULL, sku_id BIGINT UNSIGNED NOT NULL, product_title_snapshot VARCHAR(256) NOT NULL, sku_code_snapshot VARCHAR(128) NOT NULL, sku_spec_snapshot JSON NOT NULL, promotion_snapshot JSON NOT NULL, unit_price DECIMAL(12,2) NOT NULL, discount_amount DECIMAL(12,2) NOT NULL DEFAULT 0.00, quantity INT UNSIGNED NOT NULL, item_amount DECIMAL(12,2) NOT NULL, PRIMARY KEY (id), KEY idx_order_items_order (order_id), CONSTRAINT fk_order_items_order FOREIGN KEY (order_id) REFERENCES orders(id)) ENGINE=InnoDB;' | Out-Null
    $beforeChecks = @(Invoke-RootSQL -Sql "SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'CHECK';" | Where-Object { $_ -match '^\d+$' })
    if (($beforeChecks -join ',') -ne '0') { throw 'M1 baseline must not contain quantity CHECK constraints.' }
    $dsn = "app:$($env:MYSQL_PASSWORD)@tcp(127.0.0.1:$MySQLPort)/trade_db"
    & "$PSScriptRoot\apply_migrations.ps1" -MySQLDsn $dsn -ComposeFile $composeFile
    if ($LASTEXITCODE -ne 0) { throw 'First migration execution failed.' }
    & "$PSScriptRoot\apply_migrations.ps1" -MySQLDsn $dsn -ComposeFile $composeFile
    if ($LASTEXITCODE -ne 0) { throw 'Second migration execution failed.' }

    $result = @(Invoke-RootSQL -Sql "SELECT COUNT(*) FROM schema_migrations WHERE version = '20260822_m2_1_trade_schema'; SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'trade_db' AND table_name IN ('carts','cart_items','orders','order_items'); SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'FOREIGN KEY'; SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'trade_db' AND numeric_precision = 12 AND numeric_scale = 2; SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = 'trade_db' AND constraint_type = 'CHECK';" | Where-Object { $_ -match '^\d+$' })
    if (($result -join ',') -ne '1,4,2,5,2') { throw "Unexpected trade migration assertion counts: $($result -join ',')." }
    Write-Output "Verified isolated trade migration run $runID."
}
finally {
    if (-not $KeepEnvironment) { docker compose -f $composeFile down -v --remove-orphans | Out-Null }
    $env:COMPOSE_PROJECT_NAME = $previousProject
    $env:MYSQL_PORT = $previousPort
}
