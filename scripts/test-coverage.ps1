$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$summary = @()

foreach ($module in @('ledger', 'gateway')) {
    Push-Location (Join-Path $root $module)
    try {
        & go mod tidy
        if ($LASTEXITCODE -ne 0) { throw "Dependency resolution failed: $module" }
        & go test ./... -cover -coverprofile cover.out
        if ($LASTEXITCODE -ne 0) { throw "Tests failed: $module" }

        & go tool cover -html cover.out -o cover.html
        if ($LASTEXITCODE -ne 0) { throw "HTML coverage failed: $module" }

        $report = & go tool cover -func cover.out
        if ($LASTEXITCODE -ne 0) { throw "Coverage summary failed: $module" }
        $report | Write-Output
        $total = $report | Select-String '^total:\s+\(statements\)\s+([0-9.]+)%\s*$'
        if (-not $total) { throw "Coverage total missing: $module" }
        $percent = $total.Matches[0].Groups[1].Value
        $summary += "- ${module}: ${percent}% (все пакеты модуля, включая main)."
    }
    finally {
        Pop-Location
    }
}

# Обновляем README только после успешного прохождения обоих модулей.
$readmePath = Join-Path $root 'README.md'
$readme = [IO.File]::ReadAllText($readmePath)
$start = '<!-- coverage:start -->'
$end = '<!-- coverage:end -->'
$replacement = $start + "`r`n" + ($summary -join "`r`n") + "`r`n" + $end
$pattern = '(?s)' + [regex]::Escape($start) + '.*?' + [regex]::Escape($end)
if (-not [regex]::IsMatch($readme, $pattern)) { throw 'README coverage markers missing' }
$readme = [regex]::Replace($readme, $pattern, $replacement)
[IO.File]::WriteAllText($readmePath, $readme, (New-Object Text.UTF8Encoding($false)))
Write-Output 'Coverage saved to ledger/cover.html and gateway/cover.html; README updated.'
