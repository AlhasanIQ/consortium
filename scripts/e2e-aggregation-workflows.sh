#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

json_get() {
  curl -fsS "$1"
}

extract_uuid() {
  grep -Eo '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}' | head -n 1
}

need jq
need curl

BACKEND_URL="${BACKEND_URL:-$(./scripts/dev-env-value.sh BACKEND_URL http://localhost:8080 2>/dev/null)}"

./bin/conctl local restart --yes
./bin/conctl workflows seeds-matrix --output .tmp/seeds-matrix.html
./bin/conctl local db-query --sql "SELECT COUNT(*) AS workflow_count FROM workflows"

for required in \
  aggregation-collect \
  aggregation-majority-vote \
  aggregation-synthesis \
  aggregation-judge \
  aggregation-debate-decide \
  aggregation-scoring \
  aggregation-peer-matrix \
  composite-judge-synthesis-cheap \
  benchmark-informed-captain-synthesis-cheap; do
  if ! grep -q "$required" .tmp/seeds-matrix.html; then
    echo "seed matrix omitted $required" >&2
    exit 1
  fi
done

go test ./pkg/e2e -run 'TestE2EAggregation|TestE2EL|TestE2ECompiledAggregation|TestE2ECompilerRejects|TestE2EExplicitChildWorkflowStillCreatesSeparateDurableBoundary' -count=1
bun run test:e2e:builder

run_output="$(
  ./bin/conctl --timeout 15m --format json benchmarks run --yes \
    --benchmarks global-mmlu-lite \
    --workflows benchmark-informed-captain-synthesis-cheap \
    --limit 1 \
    --concurrency 1 \
    --run-set custom \
    --split dev \
    --wait \
    --interval 5s
)"

run_id="$(printf '%s\n' "$run_output" | jq -r '.id // .run_id // .Run.id // .run.id // empty' 2>/dev/null || true)"
if [[ -z "$run_id" ]]; then
  run_id="$(printf '%s\n' "$run_output" | extract_uuid || true)"
fi
if [[ -z "$run_id" ]]; then
  run_id="$(
    ./bin/conctl --format json benchmarks list --limit 20 |
      jq -r '
        .Runs[]? |
        select(.benchmark == "global-mmlu-lite") |
        select(.workflow_id == "benchmark-informed-captain-synthesis-cheap") |
        select(.split == "dev") |
        select(.item_limit == 1) |
        .id
      ' |
      head -n 1
  )"
fi
if [[ -z "$run_id" ]]; then
  echo "could not extract benchmark run id from conctl output:" >&2
  printf '%s\n' "$run_output" >&2
  exit 1
fi

./bin/conctl benchmarks get --id "$run_id"
./bin/conctl benchmarks analysis --id "$run_id" --top 5

analysis="$(json_get "$BACKEND_URL/api/admin/benchmarks/$run_id/analysis?top=5")"
printf '%s\n' "$analysis" | jq -e '.performance.agent_models | length > 0' >/dev/null
printf '%s\n' "$analysis" | jq -e '.performance.aggregation_nodes | length > 0' >/dev/null

child_job_id="$(printf '%s\n' "$analysis" | jq -r '.diagnostics.slowest_items[0].child_job_id // .diagnostics.most_retries_items[0].child_job_id // empty')"
if [[ -z "$child_job_id" ]]; then
  echo "benchmark analysis did not report a child L1 job" >&2
  exit 1
fi

child_nodes="$(./bin/conctl --format json jobs nodes --id "$child_job_id")"
printf '%s\n' "$child_nodes" | jq -e '
  [
    .. | objects |
    (
      (.metadata? // .Metadata? // {}) |
      if type == "string" then (fromjson? // {}) else . end
    ) as $metadata |
    select(($metadata.source_workflow_id? // "") | startswith("aggregation-"))
  ] | length > 0
' >/dev/null

echo "aggregation workflow smoke passed: run_id=$run_id child_job_id=$child_job_id"
