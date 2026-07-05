#!/bin/bash
# Pre-flight safety check: detect active benchmarks and running jobs before
# allowing destructive operations (server stop, restart, db-reset, etc.).
#
# Exit 0 = safe to proceed
# Exit 1 = active work detected, operation should be blocked
#
# Called from Makefile targets. Bypassed when FORCE=1 is set.

set -e

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

ORIG_PORT="${PORT-__UNSET__}"
ORIG_BACKEND_URL="${BACKEND_URL-__UNSET__}"
ORIG_DB_PATH="${DB_PATH-__UNSET__}"

set -a
if [ -f ./.env ]; then
    . ./.env
fi
if [ -f ./.env.local ]; then
    . ./.env.local
fi
set +a

if [ "$ORIG_PORT" != "__UNSET__" ]; then
    PORT="$ORIG_PORT"
fi
if [ "$ORIG_BACKEND_URL" != "__UNSET__" ]; then
    BACKEND_URL="$ORIG_BACKEND_URL"
fi
if [ "$ORIG_DB_PATH" != "__UNSET__" ]; then
    DB_PATH="$ORIG_DB_PATH"
fi

BACKEND_URL="${BACKEND_URL:-http://localhost:${PORT:-8080}}"
DB_PATH="${DB_PATH:-./consortium.db}"

BLOCKED=0
MESSAGES=""

# If the server isn't reachable, there's nothing to protect.
if ! curl -s --max-time 2 "$BACKEND_URL/health" > /dev/null 2>&1; then
    exit 0
fi

# --- Check benchmark runner ---
BENCH=$(curl -s --max-time 3 "$BACKEND_URL/api/admin/benchmarks/runner-status" 2>/dev/null || echo "")
if echo "$BENCH" | grep -q '"running":true'; then
    BLOCKED=1
    # Extract details (best-effort, falls back gracefully)
    DETAIL=$(echo "$BENCH" | python3 -c "
import sys, json
d = json.load(sys.stdin)
parts = []
if d.get('current_benchmark'): parts.append(d['current_benchmark'])
done = d.get('completed_items', 0)
total = d.get('total_items', 0)
if total: parts.append(f'{done}/{total} items')
print(' — '.join(parts) if parts else 'details unavailable')
" 2>/dev/null || echo "details unavailable")
    MESSAGES="${MESSAGES}\n  BENCHMARK RUNNING: ${DETAIL}"
fi

# --- Check active jobs ---
# Query the database directly (works with both Docker and local setups).
RUNNING=0
PENDING=0

if docker ps 2>/dev/null | grep -q consortium-server; then
    RUNNING=$(docker exec consortium-server sh -c "apk add --no-cache sqlite > /dev/null 2>&1; sqlite3 -cmd '.timeout 5000' /data/consortium.db \"SELECT COUNT(*) FROM jobs WHERE status = 'running'\"" 2>/dev/null || echo "0")
    PENDING=$(docker exec consortium-server sh -c "apk add --no-cache sqlite > /dev/null 2>&1; sqlite3 -cmd '.timeout 5000' /data/consortium.db \"SELECT COUNT(*) FROM jobs WHERE status = 'pending'\"" 2>/dev/null || echo "0")
elif [ -f "$DB_PATH" ]; then
    RUNNING=$(sqlite3 -cmd ".timeout 5000" "$DB_PATH" "SELECT COUNT(*) FROM jobs WHERE status = 'running'" 2>/dev/null || echo "0")
    PENDING=$(sqlite3 -cmd ".timeout 5000" "$DB_PATH" "SELECT COUNT(*) FROM jobs WHERE status = 'pending'" 2>/dev/null || echo "0")
fi

# Trim whitespace
RUNNING=$(echo "$RUNNING" | tr -d '[:space:]')
PENDING=$(echo "$PENDING" | tr -d '[:space:]')

if [ "$RUNNING" -gt 0 ] 2>/dev/null || [ "$PENDING" -gt 0 ] 2>/dev/null; then
    BLOCKED=1
    MESSAGES="${MESSAGES}\n  ACTIVE JOBS: ${RUNNING} running, ${PENDING} pending"
fi

# --- Report ---
if [ "$BLOCKED" -eq 1 ]; then
    echo ""
    echo "============================================================"
    echo "  BLOCKED: Active work detected on the backend server!"
    echo "============================================================"
    echo -e "$MESSAGES"
    echo ""
    echo "  To force this operation anyway, run:"
    echo "    make <target> FORCE=1"
    echo "============================================================"
    echo ""
    exit 1
fi

exit 0
