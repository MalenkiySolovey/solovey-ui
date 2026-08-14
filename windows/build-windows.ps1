# PowerShell script for building Solovey UI on Windows
param(
    [string]$Architecture = "amd64",
    [switch]$NoCGO,
    [switch]$Help
)

if ($Help) {
    Write-Host "Usage: .\build-windows.ps1 [-Architecture <arch>] [-NoCGO] [-Help]"
    Write-Host "Architectures: amd64, 386, arm64"
    Write-Host "Examples:"
    Write-Host "  .\build-windows.ps1                    # Build for amd64 with CGO"
    Write-Host "  .\build-windows.ps1 -Architecture 386 # Build for 32-bit Windows"
    Write-Host "  .\build-windows.ps1 -NoCGO            # Build without CGO"
    exit 0
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir ".."))
Set-Location -LiteralPath $repoRoot

if ($Architecture -notin @("amd64", "386", "arm64")) {
    throw "Unsupported Windows architecture: $Architecture"
}

Write-Host "Building Solovey UI for Windows ($Architecture)..." -ForegroundColor Green

# Check if Go is installed
try {
    $goVersion = go version 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Go not found"
    }
    Write-Host "Go version: $goVersion" -ForegroundColor Green
} catch {
    Write-Host "Error: Go is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Go from https://golang.org/dl/" -ForegroundColor Yellow
    Read-Host "Press Enter to exit"
    exit 1
}

# Check if Node.js is installed
try {
    $nodeVersion = node --version 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Node.js not found"
    }
    Write-Host "Node.js version: $nodeVersion" -ForegroundColor Green
} catch {
    Write-Host "Error: Node.js is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Node.js from https://nodejs.org/" -ForegroundColor Yellow
    Read-Host "Press Enter to exit"
    exit 1
}

# Build frontend
Write-Host "Building frontend..." -ForegroundColor Yellow
Push-Location frontend

try {
    Write-Host "Installing dependencies..." -ForegroundColor Cyan
    npm ci
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to install frontend dependencies"
    }

    Write-Host "Building frontend..." -ForegroundColor Cyan
    $env:SOLOVEY_UI_PROFILE = "full"
    npm run build
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to build frontend"
    }
} catch {
    Write-Host "Error: $_" -ForegroundColor Red
    Pop-Location
    Read-Host "Press Enter to exit"
    exit 1
}

Pop-Location

Remove-Item Env:SOLOVEY_UI_PROFILE -ErrorAction SilentlyContinue
node scripts/check-frontend-profile.mjs --profile full --dist frontend/dist
if ($LASTEXITCODE -ne 0) {
    throw "Frontend profile validation failed"
}

Write-Host "Generating full-profile component imports..." -ForegroundColor Yellow
node scripts/generate-component-imports.mjs --profile full --out app/components_generated.go
if ($LASTEXITCODE -ne 0) {
    throw "Failed to generate component imports"
}

# Create web/html directory
Write-Host "Creating web/html directory..." -ForegroundColor Yellow
if (!(Test-Path "web\html")) {
    New-Item -ItemType Directory -Path "web\html" -Force | Out-Null
}
Get-ChildItem -LiteralPath "web\html" -Force -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force

# Copy frontend build files
Write-Host "Copying frontend build files..." -ForegroundColor Yellow
Copy-Item "frontend\dist\*" "web\html\" -Recurse -Force

# Build backend
Write-Host "Building backend..." -ForegroundColor Yellow

# Set environment variables
$env:GOOS = "windows"
$env:GOARCH = $Architecture

if ($NoCGO) {
    $env:CGO_ENABLED = "0"
    Write-Host "Building without CGO..." -ForegroundColor Yellow
} else {
    $env:CGO_ENABLED = "1"
    Write-Host "Building with CGO..." -ForegroundColor Yellow
}

try {
    go build -ldflags "-w -s -checklinkname=0" -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale" -o sui.exe main.go
    if ($LASTEXITCODE -ne 0) {
        if (!$NoCGO) {
            Write-Host "CGO build failed, trying without CGO..." -ForegroundColor Yellow
            $env:CGO_ENABLED = "0"
            go build -ldflags "-w -s -checklinkname=0" -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale" -o sui.exe main.go
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to build backend even without CGO"
            }
            Write-Host "Built without CGO (some features may be limited)" -ForegroundColor Yellow
        } else {
            throw "Failed to build backend"
        }
    } else {
        if ($env:CGO_ENABLED -eq "1") {
            Write-Host "Built successfully with CGO" -ForegroundColor Green
        } else {
            Write-Host "Built successfully without CGO" -ForegroundColor Green
        }
    }
} catch {
    Write-Host "Error: $_" -ForegroundColor Red
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host "Build completed successfully!" -ForegroundColor Green
Write-Host "Output: sui.exe" -ForegroundColor Green

# Show file info
if (Test-Path "sui.exe") {
    $fileInfo = Get-Item "sui.exe"
    Write-Host "File size: $([math]::Round($fileInfo.Length / 1MB, 2)) MB" -ForegroundColor Cyan
    Write-Host "Created: $($fileInfo.CreationTime)" -ForegroundColor Cyan
}

Read-Host "Press Enter to exit"
