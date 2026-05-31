<#
  Launch the GUI client for an e2e test run. Like run-gui.ps1, it goes through
  an Interactive scheduled task so the window lands on session 1 (the visible
  desktop), but it is parametrized for the harness: endpoint + session are
  passed as argv, and NXTERM_TEST_HOOK (the introspection port the Go harness
  reads back over the QEMU hostfwd) is set in the launched process environment.
#>
param(
  [Parameter(Mandatory = $true)][string]$Endpoint,
  [string]$Session = "",
  [int]$HookPort = 9300
)
$ErrorActionPreference = 'Stop'
$exe      = Join-Path $env:USERPROFILE 'nxgui\publish\NxtermGui.exe'
$cmdPath  = Join-Path $env:USERPROFILE 'nxgui\scripts\run-gui-test.cmd'
$taskName = 'NxtermGuiE2E'

Get-Process NxtermGui -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue

# A wrapper .cmd sets NXTERM_TEST_HOOK (read by the app at startup) in the
# session-1 process, then launches the GUI with endpoint + session as argv.
$body = "@echo off`r`nset NXTERM_TEST_HOOK=$HookPort`r`nstart `"`" `"$exe`" $Endpoint $Session`r`n"
Set-Content -Path $cmdPath -Value $body -Encoding Ascii

$action    = New-ScheduledTaskAction -Execute $cmdPath
$principal = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\wfvm" -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName $taskName `
    -InputObject (New-ScheduledTask -Action $action -Principal $principal) -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
Write-Host "launched NxtermGui (endpoint=$Endpoint session=$Session hook=$HookPort)"
