<#
  Idempotent toolchain provisioning for the HelloApp WinUI 3 build/test inside
  the wfvm Windows VM. Safe to re-run: each step is skipped if already present.

    1. .NET 8 SDK  -> C:\dotnet  (via the official dotnet-install.ps1)
    2. WinAppDriver 1.2.1 MSI (Windows UI-Automation server for the GUI test)
    3. Developer Mode registry unlock (required by WinAppDriver)

  Run over SSH (session 0); none of these steps need the interactive desktop.
#>
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$DotnetDir   = 'C:\dotnet'
$DotnetExe   = Join-Path $DotnetDir 'dotnet.exe'
$WadExe      = 'C:\Program Files (x86)\Windows Application Driver\WinAppDriver.exe'
$WadVersion  = '1.2.1'
$WadMsiUrl   = "https://github.com/microsoft/WinAppDriver/releases/download/v$WadVersion/WindowsApplicationDriver_$WadVersion.msi"

function Step($msg) { Write-Host "==> $msg" }

# --- 1. .NET 8 SDK ----------------------------------------------------------
if (Test-Path $DotnetExe) {
    Step ".NET SDK already present ($DotnetExe)"
} else {
    Step "Installing .NET 8 SDK into $DotnetDir"
    $installer = Join-Path $env:TEMP 'dotnet-install.ps1'
    Invoke-WebRequest -UseBasicParsing -Uri 'https://dot.net/v1/dotnet-install.ps1' -OutFile $installer
    & $installer -Channel '8.0' -InstallDir $DotnetDir -NoPath
    if (-not (Test-Path $DotnetExe)) { throw ".NET SDK install failed" }
}
& $DotnetExe --version

# --- 2. WinAppDriver --------------------------------------------------------
if (Test-Path $WadExe) {
    Step "WinAppDriver already installed ($WadExe)"
} else {
    Step "Installing WinAppDriver $WadVersion"
    $msi = Join-Path $env:TEMP "WinAppDriver.msi"
    Invoke-WebRequest -UseBasicParsing -Uri $WadMsiUrl -OutFile $msi
    $p = Start-Process msiexec.exe -ArgumentList "/i `"$msi`" /quiet /norestart" -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw "msiexec failed with exit code $($p.ExitCode)" }
    if (-not (Test-Path $WadExe)) { throw "WinAppDriver install failed" }
}

# --- 3. Developer Mode ------------------------------------------------------
Step "Enabling Developer Mode"
$key = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock'
if (-not (Test-Path $key)) { New-Item -Path $key -Force | Out-Null }
New-ItemProperty -Path $key -Name 'AllowDevelopmentWithoutDevLicense' -Value 1 -PropertyType DWORD -Force | Out-Null

Step "Provisioning complete"
