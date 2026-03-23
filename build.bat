@echo off
echo Building xWiki to Confluence Migration Tool...

:: Ensure we are in the script's directory
cd /d "%~dp0"

:: Build with vendored dependencies
go build -mod=vendor -o migration.exe .

if %ERRORLEVEL% EQU 0 (
    echo.
    echo SUCCESS: migration.exe has been created.
    echo You can now transfer this file and the .env file to your offline machine.
) else (
    echo.
    echo ERROR: Build failed.
)

pause
