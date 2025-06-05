@echo off
REM Process Monitor Windows Service Uninstallation Script
REM Run as Administrator

echo ========================================
echo Process Monitor Service Uninstallation
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

echo Service Name: %SERVICE_NAME%
echo.

REM Check if service exists
sc query "%SERVICE_NAME%" >nul 2>&1
if %errorLevel% neq 0 (
    echo Service %SERVICE_NAME% does not exist or is not installed.
    echo Nothing to uninstall.
    pause
    exit /b 0
)

echo Stopping service...
sc stop "%SERVICE_NAME%"
if %errorLevel% equ 0 (
    echo Service stopped successfully.
) else (
    echo Service may already be stopped or failed to stop.
)

echo Waiting for service to stop completely...
timeout /t 5 /nobreak >nul

echo Removing service...
sc delete "%SERVICE_NAME%"
if %errorLevel% equ 0 (
    echo Service removed successfully!
) else (
    echo ERROR: Failed to remove service!
    echo The service may still be running or in use.
    echo Try stopping all related processes and run this script again.
    pause
    exit /b 1
)

echo.
echo Uninstallation completed successfully!
echo Service %SERVICE_NAME% has been removed from the system.
echo.
pause