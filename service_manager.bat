@echo off
REM Process Monitor Service Management Script
REM Run as Administrator

echo ========================================
echo Process Monitor Service Manager
echo ========================================

REM Check if running as administrator
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo ERROR: This script must be run as Administrator!
    echo Right-click and select "Run as administrator"
    pause
    exit /b 1
)

set SERVICE_NAME=ProcessMonitor

:MENU
echo.
echo Service Management Options:
echo 1. Check Service Status
echo 2. Start Service
echo 3. Stop Service
echo 4. Restart Service
echo 5. View Service Configuration
echo 6. View Service Logs (Event Viewer)
echo 7. Exit
echo.
set /p choice=Please select an option (1-7): 

if "%choice%"=="1" goto CHECK_STATUS
if "%choice%"=="2" goto START_SERVICE
if "%choice%"=="3" goto STOP_SERVICE
if "%choice%"=="4" goto RESTART_SERVICE
if "%choice%"=="5" goto VIEW_CONFIG
if "%choice%"=="6" goto VIEW_LOGS
if "%choice%"=="7" goto EXIT
echo Invalid choice. Please try again.
goto MENU

:CHECK_STATUS
echo.
echo Checking service status...
sc query "%SERVICE_NAME%"
if %errorLevel% neq 0 (
    echo Service %SERVICE_NAME% is not installed.
)
goto MENU

:START_SERVICE
echo.
echo Starting service...
sc start "%SERVICE_NAME%"
if %errorLevel% equ 0 (
    echo Service started successfully!
) else (
    echo Failed to start service. Check Event Viewer for details.
)
goto MENU

:STOP_SERVICE
echo.
echo Stopping service...
sc stop "%SERVICE_NAME%"
if %errorLevel% equ 0 (
    echo Service stopped successfully!
) else (
    echo Failed to stop service or service is already stopped.
)
goto MENU

:RESTART_SERVICE
echo.
echo Restarting service...
echo Stopping service...
sc stop "%SERVICE_NAME%"
timeout /t 3 /nobreak >nul
echo Starting service...
sc start "%SERVICE_NAME%"
if %errorLevel% equ 0 (
    echo Service restarted successfully!
) else (
    echo Failed to restart service. Check Event Viewer for details.
)
goto MENU

:VIEW_CONFIG
echo.
echo Service Configuration:
sc qc "%SERVICE_NAME%"
if %errorLevel% neq 0 (
    echo Service %SERVICE_NAME% is not installed.
)
goto MENU

:VIEW_LOGS
echo.
echo Opening Event Viewer to view service logs...
echo Look for events from source "ProcessMonitor" in:
echo - Windows Logs ^> Application
echo - Windows Logs ^> System
eventvwr.msc
goto MENU

:EXIT
echo.
echo Goodbye!
exit /b 0