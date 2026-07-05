#!/usr/bin/env bash
set -euo pipefail

PID_FILE="${1:?usage: stop-process-tree.sh <pid-file> <label> [port]}"
LABEL="${2:?usage: stop-process-tree.sh <pid-file> <label> [port]}"
PORT="${3:-}"

collect_tree() {
  local root="$1"
  local child
  echo "$root"
  while IFS= read -r child; do
    [[ -z "$child" ]] && continue
    collect_tree "$child"
  done < <(pgrep -P "$root" 2>/dev/null || true)
}

process_running() {
  local pid="$1"
  kill -0 "$pid" 2>/dev/null
}

if [[ ! -f "$PID_FILE" ]]; then
  echo "No $PID_FILE file found"
  if [[ -n "$PORT" ]] && lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "Port $PORT is in use by another process, but this worktree does not own it."
  elif [[ -n "$PORT" ]]; then
    echo "No $LABEL running on port $PORT"
  else
    echo "No $LABEL running"
  fi
  exit 0
fi

PID="$(cat "$PID_FILE" 2>/dev/null | tr -d '[:space:]' || true)"
if [[ ! "$PID" =~ ^[0-9]+$ ]]; then
  echo "$LABEL PID file is invalid: $PID_FILE"
  rm -f "$PID_FILE"
  exit 1
fi

if ! process_running "$PID"; then
  echo "$LABEL not running (stale PID file)"
  rm -f "$PID_FILE"
  exit 0
fi

echo "Stopping $LABEL (PID: $PID)..."

mapfile -t TREE_PIDS < <(collect_tree "$PID" | awk '!seen[$0]++')
PGID="$(ps -o pgid= -p "$PID" 2>/dev/null | tr -d '[:space:]' || true)"
SELF_PGID="$(ps -o pgid= -p "$$" 2>/dev/null | tr -d '[:space:]' || true)"

if [[ -n "$PGID" && "$PGID" != "$SELF_PGID" ]]; then
  kill -TERM "-$PGID" 2>/dev/null || true
else
  for tree_pid in "${TREE_PIDS[@]}"; do
    kill -TERM "$tree_pid" 2>/dev/null || true
  done
fi

for _ in {1..20}; do
  any_running=0
  for tree_pid in "${TREE_PIDS[@]}"; do
    if process_running "$tree_pid"; then
      any_running=1
      break
    fi
  done
  [[ "$any_running" -eq 0 ]] && break
  sleep 0.1
done

any_running=0
for tree_pid in "${TREE_PIDS[@]}"; do
  if process_running "$tree_pid"; then
    any_running=1
    break
  fi
done

if [[ "$any_running" -eq 1 ]]; then
  echo "Force killing $LABEL process tree..."
  if [[ -n "$PGID" && "$PGID" != "$SELF_PGID" ]]; then
    kill -KILL "-$PGID" 2>/dev/null || true
  else
    for tree_pid in "${TREE_PIDS[@]}"; do
      kill -KILL "$tree_pid" 2>/dev/null || true
    done
  fi
fi

for _ in {1..20}; do
  any_running=0
  for tree_pid in "${TREE_PIDS[@]}"; do
    if process_running "$tree_pid"; then
      any_running=1
      break
    fi
  done
  [[ "$any_running" -eq 0 ]] && break
  sleep 0.1
done

for tree_pid in "${TREE_PIDS[@]}"; do
  if process_running "$tree_pid"; then
    echo "Failed to stop $LABEL process $tree_pid" >&2
    exit 1
  fi
done

rm -f "$PID_FILE"
echo "✅ $LABEL stopped"
