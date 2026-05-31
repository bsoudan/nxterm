<# Dump recent app-crash diagnostics for Nx2Gui from the machine-wide Application
   event log (session-independent, unlike %TEMP% redirects). #>
$ErrorActionPreference = 'SilentlyContinue'
Write-Host "=== Application Error / .NET Runtime / WER (last 15 min) ==="
Get-WinEvent -FilterHashtable @{LogName='Application'; StartTime=(Get-Date).AddMinutes(-15)} |
  Where-Object {
    $_.ProviderName -in @('Application Error','.NET Runtime','Windows Error Reporting') -or
    $_.Message -match 'Nx2Gui|wasmtime|WindowsAppRuntime|Microsoft\.ui|0x[0-9a-fA-F]{8}'
  } |
  Select-Object -First 6 |
  ForEach-Object {
    Write-Host ("--- {0} [{1}] ---" -f $_.TimeCreated, $_.ProviderName)
    Write-Host $_.Message
    Write-Host ""
  }
Write-Host "=== WER ReportArchive (Nx2Gui) ==="
Get-ChildItem "$env:ProgramData\Microsoft\Windows\WER\ReportArchive" -Recurse -Filter 'Report.wer' |
  Where-Object { (Get-Content $_.FullName -Raw) -match 'Nx2Gui' } |
  Select-Object -First 2 |
  ForEach-Object { Write-Host "--- $($_.FullName) ---"; Get-Content $_.FullName | Select-String -Pattern 'AppName|Exception|Fault|Problem|Sig\[' }
