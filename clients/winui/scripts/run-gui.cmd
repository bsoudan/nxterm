@echo off
rem Launch the GUI pointed at the nxterm server. From inside the VM the Linux
rem host is reachable at QEMU's SLIRP alias 10.0.2.2.
if "%NXTERM_ENDPOINT%"=="" set NXTERM_ENDPOINT=10.0.2.2:7654
start "" "%USERPROFILE%\nxgui\publish\NxtermGui.exe"
