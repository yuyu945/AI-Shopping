$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot 'test_trade_migration_integration.ps1'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw 'Trade migration integration script is missing.'
}

$source = Get-Content -Raw $scriptPath
foreach ($forbidden in @('MYSQL_PWD=', '-e "MYSQL_PWD')) {
    if ($source.Contains($forbidden)) {
        throw "Trade migration integration script must not pass credentials through docker compose argv: '$forbidden'."
    }
}
if ($source -match 'docker compose[^\r\n]*\s-e\s+[^\r\n]*MYSQL_(PASSWORD|ROOT_PASSWORD)') {
    throw 'Trade migration integration script must not pass a Compose password environment variable through docker compose argv.'
}
if ($source -notmatch [regex]::Escape('MYSQL_ROOT_PASSWORD')) {
    throw 'Trade migration integration script must use the Compose-managed root password inside the MySQL container.'
}
if ($source -match '&\s+"\$PSScriptRoot\\apply_migrations\.ps1"[^\r\n]*-MySQLDsn') {
    throw 'Trade migration integration script must not pass a DSN to apply_migrations.ps1 through child argv.'
}
foreach ($required in @('$env:AI_SHOPPING_MYSQL_DSN = $tradeDsn', '$env:AI_SHOPPING_MYSQL_DSN = $catalogDsn', '& "$PSScriptRoot\apply_migrations.ps1" -ComposeFile $composeFile', '& "$PSScriptRoot\apply_catalog_migrations.ps1" -ComposeFile $composeFile', '$previousMigrationDsn', '$hadMigrationDsn', 'Remove-Item Env:AI_SHOPPING_MYSQL_DSN')) {
    if ($source -notmatch [regex]::Escape($required)) {
        throw "Trade migration integration script must preserve inherited DSN handling: '$required'."
    }
}
foreach ($required in @('20260822_m2_2_payment_reservation', 'inventory_reservations', 'payment_attempt_id', 'reservation_id', 'payment_started_at', 'Legacy M2.1 pre-check schema unexpectedly contains M2.2 persistence')) {
    if ($source -notmatch [regex]::Escape($required)) {
        throw "Trade migration integration script must assert M2.2 persistence: '$required'."
    }
}

function Assert-CleanupRestoresEnvironment {
    param(
        [string]$CaseName,
        [hashtable]$InitialEnvironment
    )

    $trackedVariables = @('COMPOSE_PROJECT_NAME', 'MYSQL_PORT', 'AI_SHOPPING_MYSQL_DSN')
    $savedEnvironment = @{}
    foreach ($name in $trackedVariables + @('AI_SHOPPING_TRADE_MIGRATION_INTEGRATION', 'MYSQL_PASSWORD', 'MYSQL_ROOT_PASSWORD')) {
        $savedEnvironment[$name] = [pscustomobject]@{
            Exists = Test-Path "Env:$name"
            Value = [Environment]::GetEnvironmentVariable($name, 'Process')
        }
    }

    try {
        foreach ($name in $trackedVariables) {
            Remove-Item "Env:$name" -ErrorAction SilentlyContinue
        }
        foreach ($entry in $InitialEnvironment.GetEnumerator()) {
            Set-Item "Env:$($entry.Key)" $entry.Value
        }
        $env:AI_SHOPPING_TRADE_MIGRATION_INTEGRATION = '1'
        $env:MYSQL_PASSWORD = 'test-password'
        $env:MYSQL_ROOT_PASSWORD = 'test-root-password'

        $testScriptPath = Join-Path ([IO.Path]::GetTempPath()) ("trade-cleanup-{0}.ps1" -f [guid]::NewGuid().ToString('N'))
        [System.IO.File]::WriteAllText($testScriptPath, $source.Replace('docker compose -f $composeFile', 'cmd /c exit 1'))

        $caught = $null
        try {
            . $testScriptPath
        }
        catch {
            $caught = $_
        }
        if ($null -eq $caught -or $caught.Exception.Message -notmatch 'Could not start isolated MySQL Compose service') {
            throw "$CaseName must preserve the primary migration failure; got '$($caught.Exception.Message)'."
        }
        foreach ($name in $trackedVariables) {
            $expectedExists = $InitialEnvironment.ContainsKey($name)
            $actualExists = Test-Path "Env:$name"
            if ($actualExists -ne $expectedExists) {
                throw "$CaseName must restore whether $name exists."
            }
            if ($expectedExists -and (Get-Item "Env:$name").Value -ne $InitialEnvironment[$name]) {
                throw "$CaseName must restore the original $name value."
            }
        }
    }
    finally {
        Remove-Item $testScriptPath -ErrorAction SilentlyContinue
        foreach ($name in $savedEnvironment.Keys) {
            if ($savedEnvironment[$name].Exists) {
                Set-Item "Env:$name" $savedEnvironment[$name].Value
            }
            else {
                Remove-Item "Env:$name" -ErrorAction SilentlyContinue
            }
        }
    }
}

Assert-CleanupRestoresEnvironment -CaseName 'existing environment' -InitialEnvironment @{
    COMPOSE_PROJECT_NAME = 'caller-project'
    MYSQL_PORT = '4306'
    AI_SHOPPING_MYSQL_DSN = 'caller-dsn'
}
Assert-CleanupRestoresEnvironment -CaseName 'absent environment' -InitialEnvironment @{}

$finallyIndex = $source.IndexOf('finally {')
$cleanupIndex = $source.IndexOf('docker compose -f $composeFile down', $finallyIndex)
$restoreIndex = $source.IndexOf('$env:COMPOSE_PROJECT_NAME', $finallyIndex)
if ($cleanupIndex -lt 0 -or $restoreIndex -lt 0 -or $source.Substring($finallyIndex, $restoreIndex - $finallyIndex) -notmatch 'catch') {
    throw 'Integration cleanup must catch Docker down failures before restoring the environment.'
}
foreach ($required in @('$hadProject = Test-Path Env:COMPOSE_PROJECT_NAME', '$hadPort = Test-Path Env:MYSQL_PORT', 'Remove-Item Env:COMPOSE_PROJECT_NAME -ErrorAction SilentlyContinue', 'Remove-Item Env:MYSQL_PORT -ErrorAction SilentlyContinue', 'Remove-Item Env:AI_SHOPPING_MYSQL_DSN -ErrorAction SilentlyContinue', 'if ($null -ne $cleanupFailure)')) {
    if ($source -notmatch [regex]::Escape($required)) {
        throw "Integration cleanup must preserve the required recovery behavior: '$required'."
    }
}

$output = @(& pwsh -NoProfile -File $scriptPath 2>&1)
if ($LASTEXITCODE -ne 0) {
    throw 'Integration script must safely skip when its opt-in guard is absent.'
}
if (($output | Out-String) -notmatch 'SKIP') {
    throw 'Integration script must explicitly report a guarded skip.'
}

Write-Output 'Trade migration integration guard safely skips without opt-in.'
