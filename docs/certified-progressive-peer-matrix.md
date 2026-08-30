# Certified Progressive Peer Matrix

Peer-matrix aggregation is attractive because every candidate is judged by its peers instead of by one central model. Its cost, however, grows quadratically: with `N` agents the exhaustive matrix performs `N * (N - 1)` LLM evaluations.

Certified progressive evaluation keeps the same winner semantics while allowing Consortium to stop evaluating candidates that can no longer win.

This is not a confidence threshold, statistical approximation, or model-specific heuristic. Early termination only happens after a deterministic bound proves that every possible completion of the remaining 1-10 scores produces the same winner under Consortium's existing averaging and tie-breaking rules.

## Enable it

Add one opt-in field to a `peer_matrix` aggregation config:

```json
{
  "certified_early_stop": true,
  "normalization": "none",
  "max_parallel": 6,
  "temperature": 0,
  "max_tokens": 1024,
  "eval_system_prompt": "...",
  "eval_prompt": "... {{rubric}} ...",
  "rubric": [
    {
      "name": "Logical Soundness",
      "weight": 0.4,
      "description": "..."
    },
    {
      "name": "Evidence Analysis",
      "weight": 0.3,
      "description": "..."
    }
  ]
}
```

The default remains exhaustive evaluation. Existing workflows do not change unless `certified_early_stop` is explicitly set to `true`.

## The proof

Assume a candidate currently has:

- observed valid-score sum `S`
- `C` valid reviews
- `R` reviews that have not been attempted yet

Every valid peer score is constrained to the documented range `[1, 10]`.

The lowest final average that candidate can possibly reach is therefore:

```text
LB = (S + R * 1) / (C + R)
```

and the highest final average it can possibly reach is:

```text
UB = (S + R * 10) / (C + R)
```

These are adversarial bounds. The lower bound assumes every remaining valid review is `1`; the upper bound assumes every remaining valid review is `10`.

If candidate A has:

```text
LB(A) > UB(B)
```

then B cannot beat A even if every remaining review is maximally favorable to B and maximally unfavorable to A. B is permanently eliminated and future reviews *of B* can be skipped.

Eliminated agents still act as reviewers for surviving candidates. Only work that cannot affect the winner is removed.

### Why failed/invalid reviews do not invalidate the bound

The existing peer matrix excludes invalid evaluations from the final average. The proof deliberately treats every unattempted review as if it will be valid when calculating the extreme bounds.

If a future review instead becomes invalid, that score is omitted. Omitting a hypothetical value in `[1,10]` moves the final average back toward the already observed average, which is itself inside the same interval. Therefore invalid future reviews cannot move the final result outside the bound.

A candidate with no valid score yet receives no lower bound, so it cannot be eliminated merely because its first reviews failed. The algorithm stays conservative until there is enough evidence to prove dominance.

## Balanced progressive rounds

Exhaustive peer matrix is reviewer-major. Certified mode instead schedules the same ordered reviewer-candidate pairs as deterministic round-robin derangements.

For `N` agents there are `N - 1` rounds. In each round:

- every reviewer performs at most one evaluation
- every active candidate receives at most one new review
- no agent reviews itself
- across all rounds, every ordered reviewer -> candidate pair appears exactly once

If no winner can be certified, the schedule eventually evaluates the same full `N * (N - 1)` pair set as exhaustive mode.

This balanced schedule prevents an early candidate from receiving many more observations simply because of task ordering and gives the proof a predictable progression. `max_parallel` still bounds concurrency within each round.

## Deterministic tie handling

Consortium's existing peer-matrix winner selection breaks exact score ties by ascending agent ID.

Certified mode uses a strict `1e-9` dominance margin while unobserved reviews remain. It does not terminate early merely because two floating-point bounds are numerically equal.

The alphabetical tie rule is used by the certificate only after both candidates are fully observed. This makes early stopping conservative around floating-point boundaries.

## Machine-readable certificate

When certified mode runs, `eval_matrix.certificate` contains the proof state. The proof contract is explicitly versioned so downstream audit tooling can reject semantics it does not recognize:

```json
{
  "mode": "certified_progressive",
  "proof_version": "bounded-average-v1",
  "certified": true,
  "winner": "agent-a",
  "score_min": 1,
  "score_max": 10,
  "normalization": "none",
  "tie_break": "agent_id_ascending",
  "total_evaluations": 20,
  "completed_evaluations": 15,
  "skipped_evaluations": 5,
  "savings_ratio": 0.25,
  "rounds_completed": 3,
  "winner_lower_bound": 7.75,
  "strongest_challenger_upper_bound": 3.25,
  "guaranteed_margin": 4.5,
  "bounds": {
    "agent-a": {
      "observed_average": 10,
      "lower_bound": 7.75,
      "upper_bound": 10,
      "valid_reviews": 3,
      "invalid_reviews": 0,
      "remaining_reviews": 1
    },
    "agent-b": {
      "observed_average": 1,
      "lower_bound": 1,
      "upper_bound": 3.25,
      "valid_reviews": 3,
      "invalid_reviews": 0,
      "remaining_reviews": 1,
      "eliminated": true,
      "dominated_by": "agent-a"
    }
  },
  "skipped_pairs": [
    {"reviewer_id": "agent-a", "candidate_id": "agent-e"},
    {"reviewer_id": "agent-b", "candidate_id": "agent-a"},
    {"reviewer_id": "agent-c", "candidate_id": "agent-b"},
    {"reviewer_id": "agent-d", "candidate_id": "agent-c"},
    {"reviewer_id": "agent-e", "candidate_id": "agent-d"}
  ]
}
```

`skipped_pairs` identifies the exact matrix cells that were never sent to a provider. This lets the UI and external audit tooling distinguish a certified prune from an attempted evaluation that failed to produce a valid score.

The certificate is useful beyond the UI: benchmark tooling can measure call savings, audit systems can inspect why an evaluation stopped, and orchestration layers can distinguish a mathematically locked result from an ordinary partial run.

When early stopping occurs, `eval_matrix.final_scores` and the top-level `scores` contain the observed averages from reviews that actually ran. They are not fabricated estimates for skipped cells. The certificate bounds are the authoritative proof that the selected winner cannot change.

## Safety constraints

Certified mode fails closed when its proof assumptions are not satisfied:

- normalization must be `none`
- rubric weights must be finite and non-negative
- total rubric weight must be positive
- parsed criterion scores must remain inside `[1,10]`
- agent IDs must be unique
- reviewer/candidate pairs must be unique and cannot be self-reviews
- every reviewer and candidate referenced by the proof must belong to the input set
- `certified_early_stop`, when present, must be a JSON boolean

The score-domain guard also closes a legacy inconsistency in peer-matrix parsing: the text-score fallback already rejected values outside 1-10, while the JSON `scores` fallback could previously pass them through. Peer-matrix evaluation now enforces the documented 1-10 contract consistently.

These checks are intentionally stricter than the legacy exhaustive path. If a future normalization strategy changes ranking semantics, it cannot silently inherit a proof that was derived for raw averaged scores.

## Cost and latency behavior

The optimization has no guaranteed savings on every workload. If candidates remain close, certified mode can execute the full matrix. Its value is that it removes calls only when they are provably irrelevant.

For a five-agent matrix, the exhaustive maximum is 20 peer evaluations. A strongly separated winner can be locked after three balanced rounds, using 15 evaluations and skipping the final five. Larger ensembles can eliminate weak candidates progressively and avoid their remaining evaluation columns.

Progressive proof checks happen between balanced rounds. With a concurrency limit at or below the ensemble size, this generally preserves the natural wave structure of the exhaustive worker pool. With very high `max_parallel`, exhaustive mode can launch work from later rounds sooner, so certified mode can trade some wall-clock parallelism for the opportunity to avoid those calls. The exact call savings are reported per run rather than predicted in advance.

## UI behavior

The workflow builder exposes certified mode as an explicit checkbox; it is never enabled implicitly.

Completed runs show a proof card with the winner's lower bound, strongest challenger upper bound, guaranteed margin, rounds, call savings, and per-candidate bounds. The heatmap renders certified-pruned cells as `skip` and attempted-but-invalid evaluations as `!`, preserving the difference between intentional optimization and provider/parse failure.

## Test strategy

The implementation is covered at several levels:

1. Structural tests prove the progressive round scheduler covers every ordered reviewer-candidate pair exactly once with no self-reviews.
2. Adversarial unit tests cover worst-case score completions, invalid-review denominator behavior, exact tie semantics, malformed score domains, rubric constraints, duplicate/self review rejection, skipped-pair accounting, and fail-closed configuration.
3. A randomized equivalence test generates 2,000 complete peer matrices and verifies every certified early winner equals the winner produced by exhaustive evaluation of the same scripted matrix.
4. An integration test counts actual provider calls: a five-agent case that would normally execute 20 LLM evaluations must stop at exactly 15 calls, emit five exact skipped pairs, and attach the proof certificate to the returned evaluation matrix.

No new third-party dependencies are required.