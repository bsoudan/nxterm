<# Tear down the WinAppDriver scheduled task and process. Best-effort. #>
$ErrorActionPreference = 'SilentlyContinue'
$taskName = 'HelloWinAppDriver'
Stop-ScheduledTask -TaskName $taskName
Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
Get-Process WinAppDriver | Stop-Process -Force
Write-Host "WinAppDriver stopped"
