@echo off
REM Process Monitor Windows Service Installation Script
REM Run as Administrator

echo ========================================
echo Process Monitor Service Installation
echo ========================================

REM Check if running as administrator
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo ERROR: This script must be run as Administrator!
    echo Right-click and select "Run as administrator"
    pause
    exit /b 1
)

REM Get current directory
set CURRENT_DIR=%~dp0
set SERVICE_NAME=ProcessMonitor
set SERVICE_DISPLAY_NAME=Process Monitor Service
set SERVICE_DESCRIPTION=Monitors and restarts configured processes automatically
set EXECUTABLE_PATH=%CURRENT_DIR%processmonitor.exe
set CONFIG_PATH=%CURRENT_DIR%config.yaml

echo Current Directory: %CURRENT_DIR%
echo Service Name: %SERVICE_NAME%
echo Executable: %EXECUTABLE_PATH%
echo Config File: %CONFIG_PATH%
echo.

REM Check if executable exists
if not exist "%EXECUTABLE_PATH%" (
    echo ERROR: processmonitor.exe not found in current directory!
    echo Please ensure processmonitor.exe is in the same directory as this script.
    pause
    exit /b 1
)

REM Check if config file exists
if not exist "%CONFIG_PATH%" (
    echo ERROR: config.yaml not found in current directory!
    echo Please ensure config.yaml is in the same directory as this script.
    pause
    exit /b 1
)

REM Check if service already exists
sc query "%SERVICE_NAME%" >nul 2>&1
if %errorLevel% equ 0 (
    echo Service %SERVICE_NAME% already exists. Stopping and removing...
    sc stop "%SERVICE_NAME%"
    timeout /t 3 /nobreak >nul
    sc delete "%SERVICE_NAME%"
    timeout /t 2 /nobreak >nul
)

echo Installing service...
sc create "%SERVICE_NAME%" ^
    binPath= "\"%EXECUTABLE_PATH%\" -config \"%CONFIG_PATH%\"" ^
    DisplayName= "%SERVICE_DISPLAY_NAME%" ^
    start= auto ^
    depend= ""

if %errorLevel% neq 0 (
    echo ERROR: Failed to create service!
    pause
    exit /b 1
)

REM Set service description
sc description "%SERVICE_NAME%" "%SERVICE_DESCRIPTION%"

REM Configure service recovery options
echo Configuring service recovery options...
sc failure "%SERVICE_NAME%" reset= 86400 actions= restart/5000/restart/10000/restart/30000

REM Set service to restart on failure
sc config "%SERVICE_NAME%" start= auto

echo.
echo Service installed successfully!
echo.
echo Starting service...
sc start "%SERVICE_NAME%"

if %errorLevel% equ 0 (
    echo Service started successfully!
    echo.
    echo You can manage the service using:
    echo - Services.msc (Windows Services Manager)
    echo - sc start %SERVICE_NAME%
    echo - sc stop %SERVICE_NAME%
    echo - sc query %SERVICE_NAME%
) else (
    echo WARNING: Service installed but failed to start.
    echo Check the Windows Event Log for details.
    echo You can try starting it manually from Services.msc
)

echo.
echo Installation completed!
echo Service Name: %SERVICE_NAME%
echo Display Name: %SERVICE_DISPLAY_NAME%
echo Status: Use 'sc query %SERVICE_NAME%' to check status
echo.
pause