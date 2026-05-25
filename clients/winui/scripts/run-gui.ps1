<#
  Launch the GUI on the interactive desktop (session 1) so it appears on the
  SPICE/QMP display. A process started over SSH lands in session 0 with no
  visible desktop, so we go through a scheduled task with an Interactive
  principal (same approach as the WinAppDriver launcher in helloapp).
#>
$ErrorActionPreference = 'Stop'
$cmd      = Join-Path $env:USERPROFILE 'nxgui\scripts\run-gui.cmd'
$taskName = 'NxtermGuiSpike'

Get-Process NxtermGui -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue

$action    = New-ScheduledTaskAction -Execute $cmd
$principal = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\wfvm" -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName $taskName `
    -InputObject (New-ScheduledTask -Action $action -Principal $principal) -Force | Out-Null
Start-ScheduledTask -TaskName $taskName
Write-Host "launched NxtermGui on the interactive desktop"
