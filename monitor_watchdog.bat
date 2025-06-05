@echo off
:loop
tasklist /FI "IMAGENAME eq processmonitor.exe" 2>NUL | find /I /N "processmonitor.exe">NUL
if "%ERRORLEVEL%"=="1" (
    echo Process monitor not running, restarting...
    start "" "E:\develop\AI\ProcessMonitor_v2\processmonitor.exe" -config config.yaml
)
timeout /t 30 /nobreak >nul
goto loop