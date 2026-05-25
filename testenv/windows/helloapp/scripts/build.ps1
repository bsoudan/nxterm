<#
  Build the HelloApp WinUI 3 app and the UI test project inside the VM.

    - App: published self-contained + unpackaged so it is a single runnable
      folder (HelloApp.exe + bundled Windows App SDK runtime) that WinAppDriver
      can launch by path with no MSIX install.
    - Tests: built so a later `dotnet test` run is incremental.

  Echoes APP_EXE=<path> on success.
#>
$ErrorActionPreference = 'Stop'
$dotnet = 'C:\dotnet\dotnet.exe'
$root   = Join-Path $env:USERPROFILE 'helloapp'
$pub    = Join-Path $root 'publish'

Write-Host "==> Publishing HelloApp (self-contained, unpackaged)"
& $dotnet publish (Join-Path $root 'HelloApp\HelloApp.csproj') `
    -c Release -r win-x64 --self-contained -o $pub
if ($LASTEXITCODE -ne 0) { throw "app publish failed ($LASTEXITCODE)" }

$exe = Join-Path $pub 'HelloApp.exe'
if (-not (Test-Path $exe)) { throw "expected $exe not produced" }

Write-Host "==> Building HelloApp.UITests"
& $dotnet build (Join-Path $root 'HelloApp.UITests\HelloApp.UITests.csproj') -c Release
if ($LASTEXITCODE -ne 0) { throw "test build failed ($LASTEXITCODE)" }

Write-Host "APP_EXE=$exe"
