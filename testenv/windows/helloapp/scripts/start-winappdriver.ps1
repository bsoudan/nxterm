<#
  Start WinAppDriver on the interactive desktop (the autologon session), so it
  can drive the GUI of the app it launches. Done via a scheduled task with an
  Interactive principal — a process started directly over SSH lands in the
  non-interactive session 0 and cannot reach the desktop.

  Returns once the driver is accepting connections on 127.0.0.1:4723.
#>
$ErrorActionPreference = 'Stop'
$wad      = 'C:\Program Files (x86)\Windows Application Driver\WinAppDriver.exe'
$taskName = 'HelloWinAppDriver'

Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue

$action    = New-ScheduledTaskAction -Execute $wad
$principal = New-ScheduledTaskPrincipal -UserId "$env:COMPUTERNAME\wfvm" `
                -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName $taskName `
    -InputObject (New-ScheduledTask -Action $action -Principal $principal) -Force | Out-Null
Start-ScheduledTask -TaskName $taskName

for ($i = 0; $i -lt 30; $i++) {
    if ((Test-NetConnection -ComputerName 127.0.0.1 -Port 4723 -WarningAction SilentlyContinue).TcpTestSucceeded) {
        Write-Host "WinAppDriver listening on 127.0.0.1:4723"
        exit 0
    }
    Start-Sleep -Seconds 1
}
throw "WinAppDriver did not start listening on 127.0.0.1:4723"
