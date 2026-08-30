# Adaptive consensus short-circuiting

Judge aggregation normally adds one more model call after candidate generation (and can add a repair call when winner parsing fails). For discrete-answer workloads, that extra call is unnecessary when the candidate ensemble already has a strong, extractable consensus.

Consortium can now make that optimization explicit and conservative.

## Configuration

The judge aggregator accepts two optional fields in `aggregationConfig`:

```json
{
  "short_circuit_threshold": 0.8,
  "short_circuit_min_votes": 3
}
```

- `short_circuit_threshold` is the fraction of **all candidate inputs** that must produce the same extracted answer. Valid range: `0.5` through `1.0`.
- `short_circuit_min_votes` is the minimum number of agreeing extracted votes. It must be at least `2`.
- The default threshold is `1.0`, so existing workflows retain the previous unanimous-only behavior unless they opt in.

The normal answer extractor configuration is still required for a deterministic answer-level decision, for example:

```json
{
  "extraction_strategy": "regex",
  "extraction_pattern": "(?i)(?:final\\s+answer|answer)\\s*(?:is|:)?\\s*([A-Z0-9_-]+)",
  "short_circuit_threshold": 0.8,
  "short_circuit_min_votes": 3
}
```

## Why the denominator is all inputs

Suppose five agents run, but only three produce extractable answers and all three happen to agree. Treating that as `3/3 = 100%` would overstate confidence because two agents gave unusable evidence.

The quorum therefore uses `agreeing / total inputs`. The same example is `3/5 = 60%`.

This is intentionally fail-closed: missing extraction evidence makes the fast path harder, not easier, to trigger.

## Tie and safety behavior

A tied plurality never short-circuits. Invalid threshold/min-vote configuration returns `ErrAggregationConfig` instead of silently falling through to a paid judge request.

When the quorum is met, the returned `AggregationResult` exposes:

- the actual `AgreementRatio`;
- the extracted `ConsensusAnswer`;
- dissenting candidate IDs;
- a deterministic representative winner/output from the consensus camp;
- reasoning that records the quorum and that the judge LLM call was skipped.

The aggregation-stage token and cost counters remain zero because no judge request was made.

## Cost and latency impact

This optimization removes only the **judge aggregation call**. It does not claim to remove the candidate-generation calls that created the ensemble.

For a judge topology it can avoid one model call, plus a possible repair call, whenever the configured answer-level quorum is already decisive. That is especially useful in benchmark or production workloads with discrete outputs where consensus is common.

Use lower thresholds only when the extraction strategy is reliable for the task. Open-ended creative or long-form synthesis tasks should normally keep the default unanimous threshold or use a semantic evaluator instead.
