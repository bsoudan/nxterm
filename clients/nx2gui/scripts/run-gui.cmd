@echo off
REM Launch the nx2 host on the interactive desktop. %NX2_ENDPOINT% (host:port),
REM %NX2_APP%, %NX2_SESSION%, %NX2_TEST_HOOK% are read by the app.
set NX2GUI_DIR=%USERPROFILE%\nx2gui\publish
start "" "%NX2GUI_DIR%\Nx2Gui.exe"
