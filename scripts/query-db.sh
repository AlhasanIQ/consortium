#!/bin/bash
# Query the consortium database (works with both Docker and local setups)
# Usage: ./scripts/query-db.sh "SELECT * FROM jobs LIMIT 5"
#    or: ./scripts/query-db.sh (for interactive mode)

set -e

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

ORIG_DB_PATH="${DB_PATH-__UNSET__}"

set -a
if [ -f ./.env ]; then
    . ./.env
fi
if [ -f ./.env.local ]; then
    . ./.env.local
fi
set +a

if [ "$ORIG_DB_PATH" != "__UNSET__" ]; then
    DB_PATH="$ORIG_DB_PATH"
fi

DB_PATH="${DB_PATH:-./consortium.db}"

# Determine if we're using Docker or local setup
# Check if Docker is running AND the container exists (suppress errors if Docker isn't running)
if docker ps 2>/dev/null | grep -q consortium-server; then
    # Docker setup - query the database inside the container
    if [ -z "$1" ]; then
        echo "Opening interactive SQLite shell (Docker database)..."
        echo "Tip: Use .tables, .schema tablename, or run SQL queries"
        docker exec -it consortium-server sh -c "apk add --no-cache sqlite > /dev/null 2>&1; sqlite3 -cmd \".timeout 5000\" /data/consortium.db"
    else
        # Run query and output results
        # Apply a busy timeout so read queries don't fail immediately under benchmark write load.
        docker exec consortium-server sh -c "apk add --no-cache sqlite > /dev/null 2>&1; sqlite3 -cmd \".timeout 5000\" /data/consortium.db \"$1\""
    fi
elif [ -f "$DB_PATH" ]; then
    # Local setup - query the local database
    if [ -z "$1" ]; then
        echo "Opening interactive SQLite shell (local database)..."
        echo "Tip: Use .tables, .schema tablename, or run SQL queries"
        sqlite3 -cmd ".timeout 5000" "$DB_PATH"
    else
        sqlite3 -cmd ".timeout 5000" "$DB_PATH" "$1"
    fi
else
    echo "Error: No database found at $DB_PATH. Make sure consortium is running (docker or local)."
    exit 1
fi
