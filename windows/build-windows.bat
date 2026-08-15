@echo off
setlocal enabledelayedexpansion

echo Building Solovey UI for Windows...

cd /d "%~dp0.."

REM Check if Go is installed
go version >nul 2>&1
if errorlevel 1 (
    echo Error: Go is not installed or not in PATH
    echo Please install Go from https://golang.org/dl/
    pause
    exit /b 1
)

REM Check if Node.js is installed
node --version >nul 2>&1
if errorlevel 1 (
    echo Error: Node.js is not installed or not in PATH
    echo Please install Node.js from https://nodejs.org/
    pause
    exit /b 1
)

echo Building frontend...
cd frontend
call npm ci
if errorlevel 1 (
    echo Error: Failed to install frontend dependencies
    pause
    exit /b 1
)

set "SOLOVEY_UI_PROFILE=full"
call npm run build
set "FRONTEND_BUILD_EXIT=%ERRORLEVEL%"
set "SOLOVEY_UI_PROFILE="
if not "%FRONTEND_BUILD_EXIT%"=="0" (
    echo Error: Failed to build frontend
    pause
    exit /b 1
)

cd ..

node scripts\check-frontend-profile.mjs --profile full --dist frontend\dist
if errorlevel 1 (
    echo Error: Frontend profile validation failed
    pause
    exit /b 1
)

echo Generating full-profile component imports...
node scripts\generate-component-imports.mjs --profile full --out app\components_generated.go
if errorlevel 1 (
    echo Error: Failed to generate component imports
    pause
    exit /b 1
)

echo Creating web/html directory...
if exist "web\html" rmdir /s /q "web\html"
mkdir "web\html"

echo Copying frontend build files...
xcopy "frontend\dist\*" "web\html\" /E /Y /Q

echo Building backend...
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64

REM SQLite persistence requires CGO; never publish a stub-backed binary.
go build -ldflags "-w -s -checklinkname=0" -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale" -o sui.exe main.go
if errorlevel 1 (
    echo Error: Failed to build backend with the required CGO SQLite runtime
    pause
    exit /b 1
)
echo Built with CGO

echo Build completed successfully!
echo Output: sui.exe
pause
