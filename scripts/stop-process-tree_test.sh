#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR="$(mktemp -d)"
PID_FILE="$TMPDIR/parent.pid"
CHILD_PID_FILE="$TMPDIR/child.pid"

cleanup() {
  if [[ -f "$PID_FILE" ]]; then
    PID="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [[ -n "$PID" ]]; then
      PGID="$(ps -o pgid= -p "$PID" 2>/dev/null | tr -d ' ' || true)"
      if [[ -n "$PGID" ]]; then
        kill -TERM "-$PGID" 2>/dev/null || true
        sleep 0.2
        kill -KILL "-$PGID" 2>/dev/null || true
      else
        kill -TERM "$PID" 2>/dev/null || true
        sleep 0.2
        kill -KILL "$PID" 2>/dev/null || true
      fi
    fi
  fi
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

python3 - "$PID_FILE" "$CHILD_PID_FILE" <<'PY'
import os
import subprocess
import sys

pid_file, child_pid_file = sys.argv[1], sys.argv[2]
command = f"sleep 1000 & echo $! > {child_pid_file}; wait"
process = subprocess.Popen(
    ["sh", "-c", command],
    preexec_fn=os.setsid,
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)
with open(pid_file, "w", encoding="utf-8") as f:
    f.write(str(process.pid))
PY

for _ in {1..50}; do
  [[ -s "$CHILD_PID_FILE" ]] && break
  sleep 0.1
done

PARENT_PID="$(cat "$PID_FILE")"
CHILD_PID="$(cat "$CHILD_PID_FILE")"

kill -0 "$PARENT_PID"
kill -0 "$CHILD_PID"

"$ROOT_DIR/scripts/stop-process-tree.sh" "$PID_FILE" "test service"

if [[ -f "$PID_FILE" ]]; then
  echo "expected PID file to be removed" >&2
  exit 1
fi

if kill -0 "$PARENT_PID" 2>/dev/null; then
  echo "expected parent process $PARENT_PID to stop" >&2
  exit 1
fi

if kill -0 "$CHILD_PID" 2>/dev/null; then
  echo "expected child process $CHILD_PID to stop" >&2
  exit 1
fi
