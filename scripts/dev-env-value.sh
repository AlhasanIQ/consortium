#!/bin/bash

set -euo pipefail

KEY="${1:?usage: dev-env-value.sh KEY [DEFAULT]}"
DEFAULT="${2-}"
ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

cd "$ROOT_DIR"

set -a
if [ -f ./.env ]; then
	. ./.env
fi
if [ -f ./.env.local ]; then
	. ./.env.local
fi
set +a

printf '%s\n' "${!KEY:-$DEFAULT}"
