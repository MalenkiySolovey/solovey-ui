@echo off
setlocal DisableDelayedExpansion
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0control-windows.ps1"
exit /b %ERRORLEVEL%
