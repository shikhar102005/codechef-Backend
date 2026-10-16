#!/usr/bin/env bash
# ==============================================================================
# 🚀 High-Concurrency Seat Reservation & Booking System - Master Launcher (Bash)
# ==============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo ""
echo "=========================================================================="
echo "   ⚡ SEAT RESERVATION & CONCURRENCY-SAFE BOOKING PLATFORM"
echo "   Enterprise Architecture: Go + Gin + React + Vite + In-Memory/Postgres"
echo "=========================================================================="
echo ""

# 1. Environment file check
if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        echo "[*] .env file not found. Copying from .env.example..."
        cp .env.example .env
        echo "[✓] Created .env successfully."
    fi
fi

API_PORT=$(grep -E "^PORT=" .env 2>/dev/null | cut -d '=' -f2 | tr -d ' \r\n' || echo "8081")
if [ -z "$API_PORT" ]; then
    API_PORT="8081"
fi

# 2. Prerequisites
if ! command -v go &>/dev/null; then
    echo "[x] ERROR: 'go' is not found. Please install Go 1.22+."
    exit 1
fi
echo "[✓] Go: $(go version)"

if ! command -v node &>/dev/null; then
    echo "[x] ERROR: 'node' is not found. Please install Node.js 18+."
    exit 1
fi
echo "[✓] Node: $(node -v)"

if ! command -v npm &>/dev/null; then
    echo "[x] ERROR: 'npm' is not found."
    exit 1
fi
echo "[✓] npm: v$(npm -v)"

# 3. Check Frontend Dependencies
if [ ! -d "frontend/node_modules" ]; then
    echo "[*] Installing frontend dependencies..."
    (cd frontend && npm install)
    echo "[✓] Dependencies installed."
fi

mkdir -p .logs
API_LOG=".logs/api.log"
WORKER_LOG=".logs/worker.log"
FRONTEND_LOG=".logs/frontend.log"

> "$API_LOG"
> "$WORKER_LOG"
> "$FRONTEND_LOG"

PIDS=()

cleanup() {
    echo ""
    echo "[*] Stopping all services..."
    for pid in "${PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
        fi
    done
    echo "[✓] All services stopped."
}
trap cleanup EXIT INT TERM

echo ""
echo "[1/3] Starting Go Backend REST API (Port $API_PORT)..."
go run ./cmd/api > "$API_LOG" 2>&1 &
API_PID=$!
PIDS+=("$API_PID")

echo "[2/3] Starting Background Worker (Reconciliation & Outbox)..."
go run ./cmd/worker > "$WORKER_LOG" 2>&1 &
WORKER_PID=$!
PIDS+=("$WORKER_PID")

echo "[*] Waiting for API readiness on http://localhost:$API_PORT/health..."
READY=false
for i in {1..30}; do
    if curl -s -f "http://localhost:$API_PORT/health" >/dev/null 2>&1; then
        READY=true
        break
    fi
    sleep 0.5
done

if [ "$READY" = true ]; then
    echo "  [✓] Backend API is ONLINE and HEALTHY!"
else
    echo "  [!] Backend API is still initializing..."
fi

echo "[3/3] Starting React Vite Frontend Server (Port 5173)..."
(cd frontend && npm run dev) > "$FRONTEND_LOG" 2>&1 &
FRONTEND_PID=$!
PIDS+=("$FRONTEND_PID")

sleep 2

FRONTEND_URL="http://localhost:5173"
SWAGGER_URL="http://localhost:$API_PORT/swagger"

echo ""
echo "=========================================================================="
echo "   🎉 ALL SERVICES ARE RUNNING SUCCESSFULLY!"
echo "=========================================================================="
echo ""
echo "   🌐 Frontend Web UI:        $FRONTEND_URL"
echo "   ⚡ REST API Server:        http://localhost:$API_PORT"
echo "   📖 Swagger Interactive UI: $SWAGGER_URL"
echo "   💓 Health Probe:           http://localhost:$API_PORT/health"
echo "   📊 Metrics Endpoint:       http://localhost:$API_PORT/metrics"
echo ""
echo "   📋 Demo Credentials:"
echo "      • Customer 1: alice@example.com / password123"
echo "      • Customer 2: bob@example.com   / password123"
echo "      • System Admin: admin@example.com / password123"
echo ""
echo "=========================================================================="
echo " Press Ctrl+C to stop all services..."
echo ""

wait
