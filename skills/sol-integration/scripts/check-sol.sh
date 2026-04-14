#!/usr/bin/env bash
# Verify sol is installed and the sphere is running
set -euo pipefail
command -v sol >/dev/null 2>&1 || { echo "ERROR: sol not found in PATH"; exit 1; }
sol status 2>/dev/null || { echo "ERROR: sol sphere not running (try: sol up)"; exit 1; }
echo "OK: sol sphere is running"
