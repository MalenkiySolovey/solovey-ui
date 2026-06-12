@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0stop-panel.ps1" %*
