<# Publish the nx2 WinUI host (self-contained, unpackaged) inside the VM. #>
$ErrorActionPreference = 'Stop'
$dotnet = 'C:\dotnet\dotnet.exe'
$root   = Join-Path $env:USERPROFILE 'nx2gui'
$pub    = Join-Path $root 'publish'

Write-Host "==> Publishing Nx2Gui (self-contained, unpackaged)"
& $dotnet publish (Join-Path $root 'Nx2Gui\Nx2Gui.csproj') `
    -c Release -r win-x64 --self-contained -o $pub
if ($LASTEXITCODE -ne 0) { throw "publish failed ($LASTEXITCODE)" }

$exe = Join-Path $pub 'Nx2Gui.exe'
if (-not (Test-Path $exe)) { throw "expected $exe not produced" }
Write-Host "APP_EXE=$exe"
