#!/usr/bin/env bash
# Stop what testacc-up.sh started.
set -uo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
state="${STATE_DIR:-$here/.testacc}"
if [ -f "$state/api.pid" ]; then
  pid="$(cat "$state/api.pid")"
  kill "$pid" 2>/dev/null || taskkill //F //PID "$pid" >/dev/null 2>&1 || true
  rm -f "$state/api.pid"
fi
# Windows: nohup'd binaries may outlive their MSYS pid.
command -v taskkill >/dev/null 2>&1 && powershell -NoProfile -Command \
  "Get-Process transcdr-tfacc -ErrorAction SilentlyContinue | Stop-Process -Force" 2>/dev/null
docker rm -f tf-transcdr-localstack >/dev/null 2>&1 || true
echo "Stopped."
