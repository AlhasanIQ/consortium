# Reasoning Workflow Architecture

How Consortium's reasoning workflows are structured, why they're designed this way, and the principles behind each choice.

## Four-Layer Structure

Reasoning workflows use a layered architecture that separates reusable aggregation, user-facing reasoning, composition, and benchmark-specific concerns:

| Layer | Prefix | Purpose | Example |
|-------|--------|---------|---------|
| **L0 — Aggregation workflows** | `aggregation-` | Reusable aggregation internals: collection, extraction, judging, scoring, synthesis, peer review, and selection. Builder/admin only; not directly shown in Ensemble. | `aggregation-synthesis`, `aggregation-majority-vote`, `aggregation-peer-matrix` |
| **L1 — Primitives** | `reasoning-` | General-purpose reasoning strategies. Benchmark-agnostic, usable by any caller. | `reasoning-informed-captain-synthesis`, `reasoning-judge-pick` |
| **L2 — Composites** | `composite-` | Orchestrate multiple L1 primitives. | `composite-judge-synthesis-cheap` |
| **L3 — Benchmark wrappers** | `benchmark-` | Thin harness: calls an L1 child and adds benchmark-specific output contract (MCQA and non-MCQA variants). | `benchmark-informed-captain-synthesis`, `benchmark-math-informed-captain-synthesis-cheap` |

**Why four layers?** L0 aggregation workflows make method internals reusable and inspectable without turning them into user-facing products. The L1 primitives are the product — they power the Round Table UI, the API, and any future routing. Benchmark wrappers are a testing concern only. Mixing benchmark-specific instructions (like "answer with a single letter") into the primitives would degrade them for general use. The wrapper layer keeps that separation clean.

MCQA L3 wrappers use `input → child_workflow(L1) → contract_extract → result(collect)` so the canonical output is a single option label. Non-MCQA wrappers (for example `benchmark-math-*-cheap`) are thinner: `input → child_workflow(L1) → result(collect)` and grading logic parses/evaluates expressions in the benchmark harness. Benchmarks must evaluate L1 workflows, not L0 aggregation workflows directly; L0 is implementation structure, while L1 is the benchmarkable product behavior.

## Design Principles

### 1. Blind Evaluation

Aggregators that present responses to an LLM anonymize agent identities before the model sees them. Judge, scoring, synthesis, and peer-review relabel responses with letters (A, B, C...); `debate_decide` instead groups answers into neutrally-named camps (CampAlpha, CampBeta...) kept distinct from MCQA option letters. The real agent IDs are remapped after the LLM responds.

**Why:** LLMs have documented biases toward familiar model names. A judge may favor a response labeled "GPT-4" over an identical response labeled "Unknown Model". Anonymization eliminates this confound. Evaluation quality should depend on content, not brand.

### 2. Model Diversity Over Sampling Randomness

Each reasoning workflow uses agents from different model architectures (e.g., Gemini, Mimo, Grok) rather than calling the same model multiple times at high temperature.

**Why (Condorcet's Jury Theorem):** Ensemble accuracy improves when voters make *independent* errors. Duplicate models have perfectly correlated errors — you pay for 3 calls but get ~1.5 models of effective diversity. Different architectures trained on different data with different inductive biases produce genuinely independent error patterns. Model diversity is the primary source of variation, so agent temperature is kept low.

**Exception:** `reasoning-self-consistency-majority-pick` intentionally uses a single model 3x at elevated temperature. This captures *sampling diversity* (different reasoning paths from the same model), which is a valid and cheaper alternative when you want to test one strong model's consistency.

### 3. Evaluator Independence

Evaluator models should ideally differ from all answering agents to avoid self-evaluation bias. In practice, full independence is a goal, but tuned model assignments may reuse a strong evaluator when quality, cost, or availability makes that the best tradeoff. Exact model assignments are tuned through benchloop and may change.

**Why:** If the same model answers and judges, it's biased toward its own reasoning patterns. An independent evaluator assesses content on its merits rather than recognizing its own style.

### 4. Dynamic Task-Specific Rubrics

Scoring and peer-matrix aggregators support `rubric_mode: "dynamic"` — an LLM generates a task-appropriate scoring rubric from the question alone (before seeing any answers), then each response is scored against that rubric.

**Why:** A static rubric (Accuracy 30%, Completeness 25%, Clarity 25%, Relevance 20%) can't discriminate across task types. A math proof should weight logical correctness; a legal analysis should weight thoroughness; a creative task should weight originality. The dynamic rubric adds one LLM call per aggregation (not per response) for a ~15-25% aggregation cost increase, but aggregation is already the cheapest part of the pipeline.

## Model Composition

Both tiers select 3 agent models from different architectures and an evaluator model. Exact model assignments are tuned through benchloop — the examples below show current choices, not permanent fixtures.

### Standard Tier (maximize quality)

| Role | Example | Selection criteria |
|------|---------|-------------------|
| Agent 1 | `xiaomi/mimo-v2.5-pro` | Three distinct model architectures |
| Agent 2 | `deepseek/deepseek-v4-pro` | for maximum error independence |
| Agent 3 | `minimax/minimax-m3` | |
| Evaluator | `deepseek/deepseek-v4-pro` | Stronger model; independence is preferred but tuned empirically |

### Cheap Tier (maximize diversity at lowest cost)

| Role | Example | Selection criteria |
|------|---------|-------------------|
| Agent 1 | `xiaomi/mimo-v2.5` | Three distinct architectures from |
| Agent 2 | `deepseek/deepseek-v4-flash` | the cheapest available models |
| Agent 3 | `minimax/minimax-m3` | |
| Evaluator | `deepseek/deepseek-v4-pro` | Stronger evaluator for judge, scoring, synthesis, and tie-breaks |

### Debate (3 agents, 2 rounds)

Same 3-agent pool as other multi-model primitives. R1 answers independently; R2 defends and challenges. The judge follows the tier's evaluator selection at low reasoning effort.

## Aggregation Method Selection

When to use which method and the tradeoffs. **The LLM Calls column counts only the calls made _inside aggregation_** — the agents produce their answers in upstream nodes first (typically 3 calls), and these methods then decide among those existing answers. So a method showing "0–1" still sits on top of the 3 agent answers; the count is just what the aggregation step itself adds.

| Method | LLM Calls (aggregation only) | Best For | Key Tradeoff |
|--------|-----------|----------|--------------|
| `majority_vote` | 0–1† | Discrete-answer tasks (MCQA, yes/no, classification) | Zero cost on a clear majority; needs extractable answers — a tie or no-extraction falls back to the configured tie-breaker (`synthesis` = 1 call, `first`/`error` = 0) |
| `debate_decide` | 1 top-level (+up to 2 conditional branch jobs in preview) | Discrete answers with disagreement worth examining | Groups answer camps, can synthesize when no camps are extractable, and can select the agreed camp output; the compiled judge job remains visible and scheduled |
| `judge` | 1* top-level (+1 repair branch in preview) | Quick winner selection, open-ended tasks | Fast and cheap but single point of failure |
| `synthesis` | 1 | Creating unified answers that combine insights from all agents | Output is new generated text, not any agent's original response |
| `scoring` | N* (+1‡) | Rubric-based evaluation where you want scores, not just a winner | More expensive but gives per-agent scores and supports dynamic rubrics |
| `peer_matrix` | N×(N-1)* (+1‡) | Evaluation by diverse evaluators (each agent's own model scores others) | Most expensive; per-agent-model routing provides true evaluator diversity |
| `collect` | 0 | Concatenation only, no evaluation | No intelligence, just joins outputs |

`*` When all agent answers are extractable and identical, compiled L0 workflows preserve a visible unanimous fallback selection so the agreed candidate output wins deterministically. Evaluator jobs are still part of the compiled DAG and remain visible in Builder expansion.
`†` `tie_breaker_method` is required config (no implicit default); the shipped `reasoning-majority-pick` seed uses `synthesis`.
`‡` Add one LLM call for rubric generation when `rubric_mode: "dynamic"` — once per aggregation, not per response.
For `judge`, Builder expansion also reports a conditional repair prompt job that runs only when the selector output cannot be parsed into a valid winner. For `debate_decide`, Builder expansion reports both conditional branch jobs: no-camps synthesis when no extractable camp exists, and repair selection when the camp judge output cannot be parsed.

**Workflow Builder representation:** Aggregation can be authored as a visible `aggregation` node between answer producers and a presentation `result` node. New builder and seed workflows set `aggregationWorkflowId` to an L0 `aggregation-*` workflow; submit/conversion turns the macro into a compiled `workflow_ref` so the aggregation internals execute as visible operation/prompt/result nodes in the parent frozen DAG. The L0 seed JSON is a descriptor/provenance artifact, not the execution or frontend-fork source of truth; the backend compiler owns the compiled DAG shape used by execution, **Expand**, and **Fork**. Upstream answer node IDs remain bound for candidate outputs and model inheritance, which matters for `peer_matrix` because each reviewer model comes from the upstream node's `__model` context entry rather than from a model selected on the aggregation node. Legacy result-owned aggregation remains valid for imported workflows without an L0 source, but macro-authored workflows keep aggregation config on the `aggregation` node and output naming/format on the downstream `result`.

**Evaluator role by method:** The cognitive demand on the evaluator varies significantly across methods. A `judge` must reason through competing argument chains to pick a winner. A `scoring` evaluator must assess each response independently against defined criteria. A `synthesis` evaluator must understand, integrate, and generate new text. A `debate_decide` judge evaluates answer camps while fallback nodes preserve deterministic consensus selection. A `peer_matrix` distributes evaluation across multiple models. Benchmark `contract_extract` nodes (in L3 wrappers) only extract a letter from existing text via regex — no LLM reasoning required in the common case. These role differences naturally affect what reasoning effort and model capability each method needs, but optimal settings are determined empirically through benchloop.

## Workflow Primitives

Each L1 primitive captures a distinct reasoning strategy — a specific hypothesis about how multiple models (or multiple samples) can produce better answers than one. Together they cover the useful strategy space without redundancy: each exists because it tests something no other primitive tests. This section explains what makes each one unique and what must be preserved during tuning to prevent it from silently collapsing into a different primitive.

### reasoning-majority-pick

3 agents → majority_vote — **3 agent calls (L1) + 0 aggregation (L0) = 3 total** (a tie falls back to synthesis, +1)

**L0 used:** `majority_vote` method · L0 workflow `aggregation-majority-vote`

Multiple models answer independently. Answers are extracted and counted — no LLM evaluates anything. If each model is independently more likely right than wrong, the majority is more reliable than any individual (Condorcet's Jury Theorem).

**What makes it unique:** Zero-cost aggregation. No LLM is involved after the agents answer. This is the baseline — it isolates whether multi-model diversity alone improves accuracy, without any evaluation intelligence.

> **Tuning invariant:** Aggregation must stay purely mechanical (extraction + counting). Adding LLM reasoning to the aggregation would make this a more expensive judge.

### reasoning-self-consistency-majority-pick

1 agent × 3 samples (elevated temperature) → majority_vote — **3 agent calls (L1) + 0 aggregation (L0) = 3 total** (tie → synthesis, +1)

**L0 used:** `majority_vote` method · L0 workflow `aggregation-majority-vote`

One model answers the same question three times at higher temperature, producing different reasoning paths. If it reaches the same answer through different routes, confidence in that answer is high.

**What makes it unique:** The only single-model primitive. Tests whether one model's internal variance is sufficient — different reasoning paths from the same weights — rather than requiring multiple architectures.

> **Tuning invariant:** Must use a single model. Temperature must be high enough to produce genuinely different reasoning paths. Multiple models makes this vote; too-low temperature makes all samples identical.

### reasoning-judge-pick

3 agents → judge selects winner — **3 agent calls (L1) + 1 top-level aggregation (L0) = 4 normal total** (Builder preview shows 5 LLM jobs including the conditional repair branch)

**L0 used:** `judge` method · L0 workflow `aggregation-judge`

Multiple models answer independently. One independent evaluator reads all responses and picks the best, assessing the full reasoning chain — not just the final answer.

**What makes it unique:** Cheapest LLM-based evaluation. A single evaluator can catch logical errors, unjustified leaps, and self-contradictions that pure answer extraction misses. A well-reasoned minority answer can beat a poorly-reasoned majority.

> **Tuning invariant:** The judge must evaluate reasoning quality, not default to the majority answer. A judge that always picks the most common answer is a more expensive vote.

### reasoning-judge-score-pick

3 agents → scorer evaluates each — **3 agent calls (L1) + N aggregation (L0) = 6 static / 7 dynamic total** (N=3; full seed uses dynamic rubric, cheap seed uses static)

**L0 used:** `scoring` method · L0 workflow `aggregation-scoring`

Multiple models answer independently. Each response is scored against defined criteria (e.g., accuracy, completeness, clarity), producing per-response score profiles rather than just a winner.

**What makes it unique:** The only primitive that produces per-criteria scores. You learn not just *which* response is best, but *why* and on which dimensions. Supports dynamic rubrics that adapt scoring criteria to the task type.

> **Tuning invariant:** Each response must be scored independently against criteria, not compared to other responses (that's judge). Collapsing to a single "is this right?" dimension makes this a more expensive vote.

### reasoning-peer-score-pick

3 agents → each agent's model scores every other response — **3 agent calls (L1) + N×(N-1) aggregation (L0) = 9 static / 10 dynamic total** (N=3, so 6 peer evals; full seed uses dynamic rubric, cheap seed uses static)

**L0 used:** `peer_matrix` method · L0 workflow `aggregation-peer-matrix`

Multiple models answer independently. Then each agent's underlying model evaluates the other agents' responses. Each evaluation is routed to the reviewing agent's own model, providing true evaluator diversity — not just evaluation redundancy. Scores are aggregated across all evaluators to pick a winner.

**What makes it unique:** The only primitive with multiple evaluator perspectives. Instead of trusting one judge, the same diverse models that answered also evaluate each other — different models may catch different flaws. Evaluators are truly blind and do not see their own response.

**Model routing:** Each agent's model is propagated through the workflow context (`__model` keys) and used for that agent's evaluations. Peer-review routing is strict: each reviewer must have a model configured. A separate `rubric_model` config controls dynamic rubric generation and is required when `rubric_mode` is `dynamic`; dynamic scorer/reviewer prompts must include `{{rubric}}` so the generated rubric is actually used.

**Per-criterion scoring:** Each peer evaluation returns per-criterion reasoning and scores (e.g., `{"logical_soundness": {"reasoning": "...", "score": 8}, ...}`). The weighted total is computed deterministically from the rubric weights — not estimated by the LLM. Fallback parsing supports legacy single-score format (`{"score": N}`) for backward compatibility.

> **Tuning invariant:** Each agent's model must evaluate independently and must not see its own response. Routing all evaluations through a single model would make this a more expensive scored.

### reasoning-informed-captain-synthesis

3 agents → synthesizer generates new text — **3 agent calls (L1) + 1 aggregation (L0) = 4 total**

**L0 used:** `synthesis` method · L0 workflow `aggregation-synthesis`

Multiple models answer independently. A synthesizer reads all responses and creates a new, unified answer combining the best insights from each. The output is new text, not any agent's original response.

**What makes it unique:** The only primitive where aggregation creates new content. Every other primitive selects, scores, or counts existing responses. Synthesis can merge complementary strengths — one agent's structure with another's insight — producing an answer better than any individual.

> **Tuning invariant:** The synthesizer must produce new text that integrates multiple responses. If the prompt tells it to pick the best or majority answer, it becomes a more expensive judge.

### reasoning-adversarial-defense-judge-pick

3 agents R1 (independent) → 3 agents R2 (adversarial defense) → judge decides — **6 agent calls (L1: 3 R1 + 3 R2) + 1 top-level judge (L0) = 7 normal total** (Builder preview shows 8 LLM jobs including the conditional repair branch)

**L0 used:** `judge` method · L0 workflow `aggregation-judge`

Three models answer independently in round 1. In round 2, each agent sees the others' answers and must defend its own position while challenging the others — citing specific errors, unsupported claims, or logical gaps. A judge evaluates the defenses and picks the winner. **This is the only primitive with a genuine multi-round exchange between agents** — contrast `reasoning-camp-split-judge-pick`, whose `debate_decide` aggregation has no live back-and-forth (see below).

**What makes it unique:** The only primitive with adversarial pressure after independent answering. Unlike deliberation (which encourages convergence — "reconsider your answer"), debate encourages divergence — agents must defend and attack. This surfaces objections and weaknesses that convergent discussion might paper over. An agent whose defense withstands challenge is expressing high confidence; a weak defense reveals reasoning gaps.

> **Tuning invariant:** R1 must be independent (no agent sees others). R2 must be adversarial — agents defend their own position and challenge others. If R2 asks agents to "reconsider" or "update beliefs," this becomes deliberation. If R2 is removed entirely, this becomes a more expensive judge.

### reasoning-camp-split-judge-pick

3 agents → grouped by answer into camps → debate_decide renders the verdict — **3 agent calls (L1) + 1 top-level aggregation (L0) = 4 normal total** (Builder preview shows 6 LLM jobs including the conditional no-camps synthesis and repair branches)

**L0 used:** `debate_decide` method · L0 workflow `aggregation-debate-decide`

Three models answer independently, then responses are grouped by extracted answer into "camps." If all agree, the fallback selection makes that agreed answer win deterministically. If they disagree, a judge evaluates the competing positions — seeing each camp's collected reasoning, not individual responses. If no agent yields an extractable answer (e.g. free-form math), aggregation delegates to synthesis instead.

**The debate lives at the L1 layer; the verdict is L0.** The three L1 agents generate the competing positions — grouped by answer into opposing camps — and that *is* the debate. The `debate_decide` method's aggregation call renders the verdict over those camps. So the debate is the 3 agent calls, decided in a single pass — agents don't rebut each other across rounds. (For a multi-round back-and-forth exchange, see `reasoning-adversarial-defense-judge-pick`.)

**What makes it unique:** Combines independent answering with structured disagreement analysis. The judge evaluates *positions with supporting arguments* rather than individual responses. The unanimous fallback keeps consensus deterministic, while the compiled judge job remains visible for a faithful DAG preview.

> **Tuning invariant:** Agents must answer independently (not knowing about camps). The judge must be able to override the majority camp if the minority's reasoning is stronger — always picking the larger camp is a more expensive vote.

### reasoning-multi-round-majority-pick

3 agents answer (R1) → each sees others' answers and revises (R2) → majority_vote — **6 agent calls (L1: 3 R1 + 3 R2) + 0 aggregation (L0) = 6 total** (tie → synthesis, +1)

**L0 used:** `majority_vote` method · L0 workflow `aggregation-majority-vote`

Two rounds. In round 1, agents answer independently. In round 2, each agent sees the others' round 1 answers and may revise. Final answers come from majority vote on round 2 responses.

**What makes it unique:** The only multi-round primitive. Agents can change their minds after seeing peer reasoning. An agent that maintains its position after seeing counterarguments is expressing high confidence; an agent that switches found a flaw in its own reasoning.

> **Tuning invariant:** R1 must be independent (no agent sees others). R2 must show each agent the others' R1 responses and allow revision. Without peer exposure in R2, this is vote at double the cost. Forcing convergence in R2 loses the signal from agents that hold their position.

## Key Design Decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Agent temperature | Low (tuned via benchloop) | Model diversity is the primary source of variation |
| Aggregator temperature | Tuned via benchloop per method | Different aggregation tasks may benefit from different temperature profiles |
| Aggregator reasoning effort | Tuned via benchloop per method | Different aggregation tasks have different cognitive demands |
| Blind evaluation | Always on, unconditional | Eliminates model-name bias; no reason to ever expose names to the evaluator |
| Peer-matrix evaluation | Truly blind — reviewers do not see their own answer | Consistent with blind evaluation principle; eliminates anchoring to own reasoning |
| Default normalization | `"none"` (no centering) | Centering by reviewer was correcting for bias that doesn't exist when all evaluations go through one model |
| Judge/debate fallback on parse failure | Repair call → fail (triggers node retry) | Silent fallback (first-agent or largest-camp) systematically biases results; explicit failure gives the retry policy a clean signal |
| Scoring parse failure | Return 0.0 + metadata flag | Returning midpoint (5.0) hides evaluator failures with passing grades |
| System prompts | Structured reasoning prompt on agents, MCQA-specific formatting in benchmark wrappers only | General prompts belong in primitives; benchmark-specific instructions belong in wrappers |

## Tuning Pitfalls

Known failure modes that silently degrade a primitive into something it's not. Each has happened or was caught during review — they are easy to introduce accidentally.

| Pitfall | What happens | How it looks in practice |
|---------|-------------|------------------------|
| Synthesis prompt picks the majority | Collapses into a more expensive vote | Synthesizer prompt says "if responses disagree, choose the majority answer" |
| Judge defaults to popular answer | Collapses into a more expensive vote | Judge prompt says "consider which answer most agents agree on" |
| Peer-review routes through one model | Collapses into more expensive scored | All peer evaluations sent to a single model instead of each agent's own model |
| Reviewer sees own answer | Anchoring bias — reviewer defends rather than evaluates | Peer eval prompt includes "Your answer was: ..." or "Compare against your own reasoning" |
| Benchmark instructions in primitives | L1/L3 separation breaks; primitives degrade for general use | Adding "respond with a single letter A-E" to a primitive's agent system prompt |
| Self-consistency uses multiple models | Becomes vote, losing the single-model signal | Using different architectures instead of one model 3× |
| Deliberation R2 has no peer context | Becomes vote at double the cost | R2 prompt doesn't include other agents' R1 responses |
| Camp-debate judge always picks majority | Becomes a more expensive vote | Judge prompt that weighs camp size instead of reasoning quality |
| Debate R2 encourages convergence | Becomes deliberation | R2 prompt says "reconsider" or "update your answer based on others" instead of "defend and challenge" |
| Low temperature on self-consistency | All samples return identical reasoning | Temperature near 0 produces trivially identical outputs |
