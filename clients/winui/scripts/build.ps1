<# Publish the GUI client (self-contained, unpackaged) inside the VM. #>
$ErrorActionPreference = 'Stop'
$dotnet = 'C:\dotnet\dotnet.exe'
$root   = Join-Path $env:USERPROFILE 'nxgui'
$pub    = Join-Path $root 'publish'

Write-Host "==> Publishing NxtermGui (self-contained, unpackaged)"
& $dotnet publish (Join-Path $root 'NxtermGui\NxtermGui.csproj') `
    -c Release -r win-x64 --self-contained -o $pub
if ($LASTEXITCODE -ne 0) { throw "publish failed ($LASTEXITCODE)" }

$exe = Join-Path $pub 'NxtermGui.exe'
if (-not (Test-Path $exe)) { throw "expected $exe not produced" }
Write-Host "APP_EXE=$exe"
