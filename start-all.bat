@echo off
REM ==============================================================================
REM  High-Concurrency Seat Reservation & Booking System
REM  Double-click or run from CMD to launch all services.
REM ==============================================================================

setlocal enabledelayedexpansion
title Seat Reservation System Launcher

cd /d "%~dp0"

echo.
echo ==========================================================================
echo   Launching Seat Reservation and Booking Platform...
echo ==========================================================================
echo.

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-all.ps1" %*

pause
