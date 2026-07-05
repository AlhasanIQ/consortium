#!/usr/bin/env bash
set -euo pipefail

failures=0

fail() {
  printf 'release audit: %s\n' "$*" >&2
  failures=$((failures + 1))
}

tracked_file_exists() {
  git ls-files --error-unmatch "$1" >/dev/null 2>&1
}

for path in \
  ".env" \
  "consortium.db" \
  "ROADMAP.md" \
  "ROADMAP_COMPLETED.md" \
  "CLAUDE.md" \
  ".agents/skills" \
  "docs/business-strategy.md" \
  ".github/workflows/claude.yml" \
  ".github/workflows/claude-code-review.yml" \
  "benchmarks/loop/loop_memory.md" \
  "benchmarks/models_repo/models_flat.json"
do
  if tracked_file_exists "$path"; then
    fail "tracked blocker path: $path"
  fi
done

while IFS= read -r path; do
  case "$path" in
    benchmarks/results/.gitkeep|benchmarks/results/archive/.gitkeep)
      ;;
    .claude/*|docs/superpowers/*|frontend/dist/*|pkg/static/dist/*)
      fail "tracked generated/internal path: $path"
      ;;
    *.db|*.sqlite|*.pem|*.key|*.p12|*.crt)
      fail "tracked sensitive/binary artifact: $path"
      ;;
    benchmarks/results/*|benchmarks/loop/state.json|benchmarks/loop/session_index.jsonl|benchmarks/loop/sessions/*|benchmarks/loop/archive/*)
      fail "tracked benchmark runtime artifact: $path"
      ;;
    benchmarks/data/*.jsonl|benchmarks/data/*/*.jsonl)
      fail "tracked benchmark dataset artifact: $path"
      ;;
    benchmarks/models_repo/*.json)
      fail "tracked third-party model snapshot: $path"
      ;;
  esac
done < <(git ls-files)

patterns=(
  '/Users/'
  'code\.alhasan'
  'iraqsong'
  'admin@local\.test'
  'local\.test'
  'ZAVORD'
  'PROPRIETARY'
  'business-strategy'
  'go-to-market'
  'investor'
  'we sell accuracy'
  'consortium\.ai'
)

for pattern in "${patterns[@]}"; do
  if git grep -n -I -E "$pattern" -- . ':!scripts/audit-release.sh' >/tmp/consortium-audit-grep.txt 2>/dev/null; then
    cat /tmp/consortium-audit-grep.txt >&2
    fail "matched forbidden pattern: $pattern"
  fi
done
rm -f /tmp/consortium-audit-grep.txt

if [ "$failures" -ne 0 ]; then
  printf 'release audit failed with %d issue(s)\n' "$failures" >&2
  exit 1
fi

printf 'release audit passed\n'
