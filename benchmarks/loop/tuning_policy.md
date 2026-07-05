# Benchloop Tuning Policy

Human-decided constraints for benchloop tuning sessions. The benchloop agent reads this
file at the start of each session alongside the auto-derived seed identity constraints.

## Frozen Identity (Enforced Automatically)

Each reasoning seed's aggregation method and structural pattern are frozen. The benchloop
system prompt auto-derives these from `pkg/seeds/data/reasoning-*-cheap.json` and
injects them as hard constraints. No manual maintenance needed — add a new seed and the
constraint map updates automatically.

## Allowed Tuning Levers (Per Seed Category)

### All seeds
- System prompts (L1 general reasoning quality only — no benchmark-specific instructions)
- Temperature
- Reasoning effort (`openRouterReasoning.effort`)
- Timeout values
- Contract step prompts (L3 benchmark wrapper only)
- Input template on `child_workflow` node

### majority_vote seeds (vote, self-consistency)
- Extraction config (`extraction_strategy`, regex patterns)
- Tie-breaking strategy (`synthesis`, `first`, `alphabetical`, `error`)
- Sample count (self-consistency only — add/remove sample nodes, keeping single-model pattern)

### judge / debate_decide seeds (judge, debate, camp-debate, deliberation)
- Judge/aggregator model (when model swaps enabled)
- Judge/aggregator prompt and system prompt
- Judge temperature and reasoning effort
- Debate framing prompts (pro/con system prompts)

### synthesis / scoring / peer_matrix seeds
- Synthesizer/scorer model (when model swaps enabled)
- Synthesis/scoring prompt
- Scoring rubric and normalization

## Additional Human Constraints

- Skip tuning for workflows already above 93% accuracy with no obvious failure patterns
- Full `test` split re-runs require explicit human approval
- Track tuning cost separately from full re-run cost
