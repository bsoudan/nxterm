# HelloApp — WinUI 3 GUI app + WinAppDriver UI test

A minimal Windows GUI app and an automated GUI test, used to exercise the
`testenv/windows` wfvm environment with a real C# / WinUI 3 build-and-test
pipeline.

The app has a name text box and a button; entering a name (Enter or the button)
shows `hello, <name>` in a text block. The test drives that flow through the
real GUI and asserts the result.

## Layout

```
helloapp/
  HelloApp/                WinUI 3 app (unpackaged, self-contained)
    HelloApp.csproj
    app.manifest
    App.xaml / App.xaml.cs
    MainWindow.xaml / MainWindow.xaml.cs
  HelloApp.UITests/        MSTest + Appium/WinAppDriver UI test
    HelloApp.UITests.csproj
    GreetingTests.cs
  scripts/
    provision.ps1          installs .NET 8 SDK, WinAppDriver, Developer Mode (idempotent)
    build.ps1              dotnet publish (app) + dotnet build (tests)
  run-test.sh              host-side end-to-end driver (deploy/provision/build/test)
```

## Running

From the repo root, inside the dev shell:

```sh
nix develop
make test-winapp
```

`make test-winapp` runs `run-test.sh`, which:

1. starts the VM if it isn't already running (`wintest-start`);
2. deploys these sources into the guest (`wintest-deploy`);
3. provisions the toolchain — `.NET 8 SDK` to `C:\dotnet`, WinAppDriver 1.2.1,
   and the Developer Mode registry unlock (idempotent, so re-runs are cheap);
4. publishes the app (self-contained, unpackaged → a runnable `HelloApp.exe`
   folder) and builds the test project, **inside the VM**;
5. starts WinAppDriver on the **interactive desktop** (session 1) via a
   scheduled task — it can only automate the GUI from the logged-on session;
6. runs `dotnet test` from the SSH session (session 0). The test talks to
   WinAppDriver over `127.0.0.1:4723`, so the test's pass/fail exit code
   propagates straight back to `make`.

Watch it run with `wintest-view &` (SPICE) before `make test-winapp` — the app
window appears and the driver types into it.

## Why these choices

- **WinUI 3 must be built on Windows.** Its XAML compiler (`genxbf.dll`) and
  packaging tooling are Windows-native, so the build runs in the VM, not on the
  Linux host.
- **Unpackaged + self-contained** avoids MSIX install/signing and a separate
  runtime deploy: WinAppDriver launches the `.exe` by path.
- **WinAppDriver in session 1, test in session 0.** GUI automation needs the
  interactive desktop; the test process only needs an HTTP socket. Splitting
  them keeps a clean exit code on the SSH side (see `testenv/windows/README.md`
  for the session-0 vs interactive-desktop split).
- Controls carry `AutomationProperties.AutomationId` so the test finds them by
  stable id rather than by screen position.
