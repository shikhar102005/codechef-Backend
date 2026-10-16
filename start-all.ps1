# ==============================================================================
# High-Concurrency Seat Reservation & Booking System - Master Launcher
# ==============================================================================
# Usage:
#   powershell -ExecutionPolicy Bypass -File .\start-all.ps1
#   .\start-all.bat
# ==============================================================================

[CmdletBinding()]
param(
    [switch]$NoBrowser,
    [string]$Port = ""
)

$ErrorActionPreference = "Stop"
$ScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptRoot

Write-Host ""
Write-Host "==========================================================================" -ForegroundColor Cyan
Write-Host "   SEAT RESERVATION & CONCURRENCY-SAFE BOOKING PLATFORM" -ForegroundColor Yellow -BackgroundColor Black
Write-Host "   Enterprise Architecture: Go + Gin + React + Vite + In-Memory/Postgres" -ForegroundColor DarkCyan
Write-Host "==========================================================================" -ForegroundColor Cyan
Write-Host ""

# 1. Environment file (.env) verification
$envPath = Join-Path $ScriptRoot ".env"
$envExamplePath = Join-Path $ScriptRoot ".env.example"

if (-not (Test-Path $envPath)) {
    if (Test-Path $envExamplePath) {
        Write-Host "[*] .env file not found. Initializing from .env.example..." -ForegroundColor Yellow
        Copy-Item $envExamplePath $envPath
        Write-Host "[+] Created .env file successfully." -ForegroundColor Green
    } else {
        Write-Host "[!] Warning: Neither .env nor .env.example found. Proceeding with defaults." -ForegroundColor Yellow
    }
}

# Resolve Port
$apiPort = $Port
if ([string]::IsNullOrWhiteSpace($apiPort) -and (Test-Path $envPath)) {
    $portLine = Get-Content $envPath | Where-Object { $_ -match "^PORT\s*=\s*(\d+)" } | Select-Object -First 1
    if ($portLine -and ($portLine -match "^PORT\s*=\s*(\d+)")) {
        $apiPort = $matches[1]
    }
}
if ([string]::IsNullOrWhiteSpace($apiPort)) {
    $apiPort = "8081"
}

# 2. Check Prerequisites
Write-Host "[*] Verifying system prerequisites..." -ForegroundColor White

$goCmd = Get-Command "go" -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Write-Host "[x] ERROR: 'go' is not found in PATH. Please install Go 1.22+ from https://go.dev/dl/" -ForegroundColor Red
    exit 1
}
$goVer = (& go version)
Write-Host "  [+] Go Runtime: $goVer" -ForegroundColor Green

$nodeCmd = Get-Command "node" -ErrorAction SilentlyContinue
if (-not $nodeCmd) {
    Write-Host "[x] ERROR: 'node' is not found in PATH. Please install Node.js 18+ from https://nodejs.org/" -ForegroundColor Red
    exit 1
}
$nodeVer = (& node -v)
Write-Host "  [+] Node.js Runtime: $nodeVer" -ForegroundColor Green

$npmCmd = Get-Command "npm" -ErrorAction SilentlyContinue
if (-not $npmCmd) {
    Write-Host "[x] ERROR: 'npm' is not found in PATH." -ForegroundColor Red
    exit 1
}
$npmVer = (& npm -v)
Write-Host "  [+] Package Manager: npm v$npmVer" -ForegroundColor Green

# 3. Check Frontend Dependencies
$frontendDir = Join-Path $ScriptRoot "frontend"
$frontendModules = Join-Path $frontendDir "node_modules"

if (-not (Test-Path $frontendModules)) {
    Write-Host ""
    Write-Host "[*] Frontend dependencies not found. Running 'npm install' in frontend directory..." -ForegroundColor Yellow
    Push-Location $frontendDir
    try {
        & npm install
        Write-Host "[+] Frontend dependencies installed successfully." -ForegroundColor Green
    }
    finally {
        Pop-Location
    }
}

# Prepare Logs Directory
$logsDir = Join-Path $ScriptRoot ".logs"
if (-not (Test-Path $logsDir)) {
    New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
}

$apiLog = Join-Path $logsDir "api.log"
$workerLog = Join-Path $logsDir "worker.log"
$frontendLog = Join-Path $logsDir "frontend.log"
$apiErrLog = Join-Path $logsDir "api_err.log"
$workerErrLog = Join-Path $logsDir "worker_err.log"
$frontendErrLog = Join-Path $logsDir "frontend_err.log"

Set-Content -Path $apiLog -Value ""
Set-Content -Path $workerLog -Value ""
Set-Content -Path $frontendLog -Value ""
Set-Content -Path $apiErrLog -Value ""
Set-Content -Path $workerErrLog -Value ""
Set-Content -Path $frontendErrLog -Value ""

# Process tracker
$processes = New-Object System.Collections.ArrayList

function Stop-ProcessTree([int]$ProcessId) {
    if ($ProcessId -gt 0) {
        try {
            $null = Start-Process -FilePath "taskkill" -ArgumentList "/PID", $ProcessId, "/T", "/F" -NoNewWindow -Wait -PassThru -ErrorAction SilentlyContinue
        } catch {
            $null = $_
        }
    }
}

function Stop-AllServices {
    Write-Host ""
    Write-Host "[*] Stopping all services..." -ForegroundColor Yellow
    foreach ($p in $processes) {
        if ($p -and (-not $p.HasExited)) {
            Write-Host "  [-] Terminating process tree for PID $($p.Id)..." -ForegroundColor Gray
            Stop-ProcessTree -ProcessId $p.Id
        }
    }
    Write-Host "[+] All services stopped cleanly." -ForegroundColor Green
}

try {
    Write-Host ""
    Write-Host "[1/3] Starting Go Backend REST API (Port $apiPort)..." -ForegroundColor Cyan
    $apiProc = Start-Process -FilePath "go" -ArgumentList "run", "./cmd/api" -WorkingDirectory $ScriptRoot -PassThru -NoNewWindow -RedirectStandardOutput $apiLog -RedirectStandardError $apiErrLog
    [void]$processes.Add($apiProc)

    Write-Host "[2/3] Starting Background Worker (Reconciliation & Outbox)..." -ForegroundColor Cyan
    $workerProc = Start-Process -FilePath "go" -ArgumentList "run", "./cmd/worker" -WorkingDirectory $ScriptRoot -PassThru -NoNewWindow -RedirectStandardOutput $workerLog -RedirectStandardError $workerErrLog
    [void]$processes.Add($workerProc)

    # Wait for API Health Check
    Write-Host "[*] Waiting for API to become ready on http://localhost:$apiPort/health..." -ForegroundColor Gray
    $apiReady = $false
    $timeoutSeconds = 20
    $startWait = [DateTime]::UtcNow

    while (([DateTime]::UtcNow - $startWait).TotalSeconds -lt $timeoutSeconds) {
        if ($apiProc.HasExited) {
            Write-Host "[x] API Server exited prematurely!" -ForegroundColor Red
            if (Test-Path $apiLog) { Get-Content $apiLog -Tail 15 }
            if (Test-Path $apiErrLog) { Get-Content $apiErrLog -Tail 15 }
            throw "API server failed to start."
        }
        try {
            $resp = Invoke-RestMethod -Uri "http://localhost:$apiPort/health" -Method Get -TimeoutSec 1 -ErrorAction Stop
            if ($resp -and ($resp.status -eq "UP" -or $resp.status -eq "ok")) {
                $apiReady = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 400
        }
    }

    if ($apiReady) {
        Write-Host "  [+] Backend API is ONLINE and HEALTHY!" -ForegroundColor Green
    } else {
        Write-Host "  [!] Backend API is still initializing. Proceeding..." -ForegroundColor Yellow
    }

    Write-Host "[3/3] Starting React Vite Frontend Server (Port 5173)..." -ForegroundColor Cyan
    $npmExecutable = if ($IsWindows -or $env:OS -like "*Windows*") { "npm.cmd" } else { "npm" }
    $frontendProc = Start-Process -FilePath $npmExecutable -ArgumentList "run", "dev" -WorkingDirectory $frontendDir -PassThru -NoNewWindow -RedirectStandardOutput $frontendLog -RedirectStandardError $frontendErrLog
    [void]$processes.Add($frontendProc)

    Start-Sleep -Seconds 2

    $frontendUrl = "http://localhost:5173"
    $swaggerUrl = "http://localhost:$apiPort/swagger"

    if (-not $NoBrowser) {
        Write-Host "[*] Opening browser to application: $frontendUrl" -ForegroundColor Cyan
        Start-Process $frontendUrl
    }

    # Display Dashboard
    Write-Host ""
    Write-Host "==========================================================================" -ForegroundColor Green
    Write-Host "   ALL SERVICES ARE RUNNING SUCCESSFULLY!" -ForegroundColor Green -BackgroundColor Black
    Write-Host "==========================================================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "   Frontend Web UI:        $frontendUrl" -ForegroundColor White
    Write-Host "   REST API Server:        http://localhost:$apiPort" -ForegroundColor White
    Write-Host "   Swagger Interactive UI: $swaggerUrl" -ForegroundColor White
    Write-Host "   Health Probe:           http://localhost:$apiPort/health" -ForegroundColor White
    Write-Host "   Metrics Endpoint:       http://localhost:$apiPort/metrics" -ForegroundColor White
    Write-Host ""
    Write-Host "   Demo Credentials:" -ForegroundColor Cyan
    Write-Host "      - Customer 1: alice@example.com / password123" -ForegroundColor Gray
    Write-Host "      - Customer 2: bob@example.com   / password123" -ForegroundColor Gray
    Write-Host "      - System Admin: admin@example.com / password123" -ForegroundColor Gray
    Write-Host ""
    Write-Host "   Logs captured in:" -ForegroundColor DarkGray
    Write-Host "      - API:      .logs/api.log" -ForegroundColor DarkGray
    Write-Host "      - Worker:   .logs/worker.log" -ForegroundColor DarkGray
    Write-Host "      - Frontend: .logs/frontend.log" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "==========================================================================" -ForegroundColor Green
    Write-Host " Press [Q] or [Ctrl+C] to stop all services..." -ForegroundColor Yellow
    Write-Host ""

    # Monitor loop
    while ($true) {
        if ($apiProc.HasExited) {
            Write-Host "[!] API Server terminated unexpectedly (Exit Code: $($apiProc.ExitCode))" -ForegroundColor Red
            break
        }
        if ($frontendProc.HasExited) {
            Write-Host "[!] Frontend Server terminated unexpectedly (Exit Code: $($frontendProc.ExitCode))" -ForegroundColor Red
            break
        }

        try {
            if ([Console]::KeyAvailable) {
                $key = [Console]::ReadKey($true)
                if ($key.Key -eq [ConsoleKey]::Q -or ($key.Modifiers -band [ConsoleModifiers]::Control -and $key.Key -eq [ConsoleKey]::C)) {
                    break
                }
            }
        } catch {
            # Stdin is non-interactive or redirected - keep running and monitoring processes
            $null = $_
        }
        Start-Sleep -Milliseconds 400
    }
}
finally {
    Stop-AllServices
}
