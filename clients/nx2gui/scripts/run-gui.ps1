<#
  Launch the nx2 host on the interactive desktop (session 1) so it appears on the
  SPICE/QMP display. A process started over SSH lands in session 0 with no
  visible desktop, so we go through a scheduled task with an Interactive
  principal (same approach as the old client / helloapp WinAppDriver launcher).

  Pass-through env (NX2_ENDPOINT etc.) is baked into the scheduled task so the
  interactive process inherits it.
#>
$ErrorActionPreference = 'Stop'
$cmd      = Join-Path $env:USERPROFILE 'nx2gui\scripts\run-gui.cmd'
$taskName = 'Nx2GuiSpike'

Get-Process Nx2Gui -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue

# Persist pass-through env vars to the machine so the interactive task sees them.
foreach ($v in 'NX2_ENDPOINT','NX2_APP','NX2_SESSION','NX2_TEST_HOOK') {
    $val = [Environment]::GetEnvironmentVariable($v)
    if ($val) { [Environment]::SetEnvironmentVariable($v, $val, 'Machine') }
}

$action    = New-ScheduledTaskAction -Execute $cmd
$principal = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\wfvm" -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName $taskName `
    -InputObject (New-ScheduledTask -Action $action -Principal $principal) -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
Write-Host "launched Nx2Gui on the interactive desktop"
