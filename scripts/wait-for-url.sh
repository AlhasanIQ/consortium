#!/usr/bin/env bash
set -euo pipefail

URL="${1:?usage: wait-for-url.sh <url> <label> [attempts] [sleep-seconds]}"
LABEL="${2:?usage: wait-for-url.sh <url> <label> [attempts] [sleep-seconds]}"
ATTEMPTS="${3:-30}"
SLEEP_SECONDS="${4:-0.2}"

attempt=1
while [[ "$attempt" -le "$ATTEMPTS" ]]; do
  if curl -fsS -o /dev/null "$URL" >/dev/null 2>&1; then
    exit 0
  fi

  sleep "$SLEEP_SECONDS"
  attempt=$((attempt + 1))
done

echo "❌ $LABEL did not become ready at $URL after $ATTEMPTS attempts" >&2
exit 1
