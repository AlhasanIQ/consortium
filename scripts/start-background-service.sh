#!/usr/bin/env bash
set -euo pipefail

PID_FILE="${1:?usage: start-background-service.sh <pid-file> <log-file> <command...>}"
LOG_FILE="${2:?usage: start-background-service.sh <pid-file> <log-file> <command...>}"
shift 2

if [[ "$#" -eq 0 ]]; then
  echo "usage: start-background-service.sh <pid-file> <log-file> <command...>" >&2
  exit 2
fi

mkdir -p "$(dirname "$PID_FILE")" "$(dirname "$LOG_FILE")"

# TODO(v0.1-security): Service logs can contain prompts, outputs, and provider
# metadata. Deployment wrappers should create log directories with restrictive
# permissions and rotation before running shared environments.

wait_for_pid_file() {
  local pid
  for _ in {1..50}; do
    if [[ -s "$PID_FILE" ]]; then
      pid="$(cat "$PID_FILE" 2>/dev/null | tr -d '[:space:]' || true)"
      [[ "$pid" =~ ^[0-9]+$ ]] && return 0
    fi
    sleep 0.1
  done
  echo "Numeric PID file was not written: $PID_FILE" >&2
  return 1
}

if command -v python3 >/dev/null 2>&1; then
  python3 - "$PID_FILE" "$LOG_FILE" "$@" <<'PY'
import os
import subprocess
import sys

pid_file, log_file, *command = sys.argv[1:]
if not command:
    raise SystemExit("missing command")

pid_dir = os.path.dirname(pid_file) or "."
log_dir = os.path.dirname(log_file) or "."
os.makedirs(pid_dir, exist_ok=True)
os.makedirs(log_dir, exist_ok=True)

with open(log_file, "ab", buffering=0) as log:
    process = subprocess.Popen(
        command,
        stdin=subprocess.DEVNULL,
        stdout=log,
        stderr=log,
        start_new_session=True,
        close_fds=True,
    )

with open(pid_file, "w", encoding="utf-8") as f:
    f.write(str(process.pid))
PY
  wait_for_pid_file
  exit 0
fi

nohup "$@" > "$LOG_FILE" 2>&1 & echo "$!" > "$PID_FILE"
wait_for_pid_file
