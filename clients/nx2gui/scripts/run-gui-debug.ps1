<#
  Debug launcher for the nx2 host: same interactive-desktop scheduled-task path
  as run-gui.ps1, but the app's stdout/stderr are redirected to a log file and
  any .NET crash is surfaced from the Windows Application event log. Used to find
  why Nx2Gui launches but doesn't connect to the broker.
#>
$ErrorActionPreference = 'Stop'
$log     = Join-Path $env:TEMP 'nx2gui.out.log'
$err     = Join-Path $env:TEMP 'nx2gui.err.log'
$exe     = Join-Path $env:USERPROFILE 'nx2gui\publish\Nx2Gui.exe'
$taskName = 'Nx2GuiDebug'

Remove-Item $log,$err -ErrorAction SilentlyContinue
Get-Process Nx2Gui -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue

# Persist pass-through env to the machine so the interactive task inherits it.
foreach ($v in 'NX2_ENDPOINT','NX2_APP','NX2_SESSION','NX2_TEST_HOOK') {
    $val = [Environment]::GetEnvironmentVariable($v)
    if ($val) { [Environment]::SetEnvironmentVariable($v, $val, 'Machine') }
}

# Run the exe through cmd so we can redirect both streams to files.
$cmdline = "/c `"`"$exe`" >`"$log`" 2>`"$err`"`""
$action    = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument $cmdline
$principal = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\wfvm" -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName $taskName `
    -InputObject (New-ScheduledTask -Action $action -Principal $principal) -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
Write-Host "launched Nx2Gui (debug) on the interactive desktop; logs: $log / $err"
