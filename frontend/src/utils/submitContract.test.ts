import { describe, expect, it } from 'bun:test';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Edge, Node } from '@xyflow/react';
import type { AggregationConfig, AggregationMethod, NodeData } from '../types/workflow';
import type { WorkflowFileFormat } from '../types/workflowFile';
import { buildExecutionRequest } from './ensembleSync';
import { flowToWorkflow } from './workflowConverter';

/**
 * Frontend↔backend validation contract.
 *
 * The frontend has multiple canvas → execution-shape converters. Ensemble
 * submits `buildExecutionRequest` directly, while the live Builder submit path
 * sends `workflow_file` for backend conversion. `flowToWorkflow` remains a
 * direct runtime converter used by tests and reference callers. The backend
 * validates every runtime payload those paths produce. When the backend
 * tightened node validation ("no implicit defaults"), converter drift silently
 * dropped newly required fields and submits started returning 400 INVALID_WORKFLOW.
 *
 * To make that class of drift impossible to miss, this test pins the exact
 * submit payloads the direct converters emit into committed JSON fixtures under
 * `pkg/api/testdata/submit_contract/`. A Go test (submit_contract_test.go) then
 * runs those same fixtures through the REAL backend validator. The live Builder
 * workflow_file path is covered separately by workflow-file converter tests and
 * full-stack e2e submission tests. So:
 *   - backend adds/changes a node requirement  -> the Go test fails on these
 *     fixtures (the payload no longer validates).
 *   - a converter stops emitting a field       -> this test fails (the golden
 *     no longer matches) AND regenerating re-triggers the Go validation.
 *
 * Regenerate after an intentional converter change:  UPDATE_SUBMIT_CONTRACT=1 bun test
 */

// Repo-root/pkg/api/testdata/submit_contract — shared with the Go validator test.
const FIXTURE_DIR = join(import.meta.dir, '../../../pkg/api/testdata/submit_contract');
const SAMPLE_PROMPT = 'What is 2 + 2? Reply with just the number.';

function agentNode(id: string, model: string): Node<NodeData> {
  return {
    id,
    type: 'agent',
    position: { x: 0, y: 0 },
    data: {
      type: 'agent',
      label: id,
      config: {
        model,
        systemPrompt: 'You are an expert. Solve: {{user_prompt}}',
        temperature: 0.2,
        maxTokens: -1,
        timeoutSeconds: 180,
        retryPolicy: { max_attempts: 5, backoff_ms: 1000, backoff_multiply: 2, max_backoff_ms: 30000 },
      },
    },
  };
}

function syntheticResult(): Node<NodeData> {
  return {
    id: 'result-synthesis',
    type: 'result',
    position: { x: 400, y: 0 },
    data: {
      type: 'result',
      label: 'Synthesis',
      config: {
        name: 'synthesized_response',
        aggregationMethod: 'synthesis',
        aggregationConfig: {
          model: 'minimax/minimax-m2.5',
          system_prompt: 'You synthesize the strongest answer from all responses.',
          prompt: 'Question: {{prompt}}\n\nResponses:\n{{responses}}\n\nProduce one best answer.',
          temperature: 0,
          max_tokens: -1,
        },
        retryPolicy: { max_attempts: 1 },
      },
    },
  };
}

const defaultRubric = [
  { name: 'Correctness', weight: 0.7, description: 'Correct answer and reasoning' },
  { name: 'Clarity', weight: 0.3, description: 'Clear explanation' },
];

function resultMarker(id = 'result-final'): Node<NodeData> {
  return {
    id,
    type: 'result',
    position: { x: 700, y: 40 },
    data: {
      type: 'result',
      label: 'Result',
      config: { name: 'final_answer', outputFormat: 'text', retryPolicy: { max_attempts: 1 } },
    },
  };
}

function aggregationConfigFor(method: AggregationMethod): AggregationConfig {
  switch (method) {
    case 'collect':
      return { separator: '\n---\n' };
    case 'majority_vote':
      return {
        extraction_strategy: 'regex',
        extraction_pattern: 'Answer:\\s*([A-D])',
        tie_breaker_method: 'first',
      };
    case 'debate_decide':
      return {
        judge_model: 'openai/gpt-5-mini',
        system_prompt: 'You judge competing answer camps.',
        prompt: 'Question: {{prompt}}\n\n{{camps}}\n\nPick a winner.',
        extraction_strategy: 'regex',
        extraction_pattern: 'Answer:\\s*([A-D])',
        temperature: 0,
        max_tokens: -1,
        repair_max_tokens: 256,
      };
    case 'judge':
      return {
        judge_model: 'openai/gpt-5-mini',
        system_prompt: 'You judge competing responses.',
        prompt: 'Question: {{prompt}}\n\n{{responses}}\n\nPick a winner.',
        temperature: 0,
        max_tokens: -1,
        repair_max_tokens: 256,
      };
    case 'scoring':
      return {
        scoring_model: 'openai/gpt-5-mini',
        system_prompt: 'You score responses against the rubric.',
        prompt: 'Question: {{prompt}}\n\nResponse: {{response}}\n\nRubric:\n{{rubric}}',
        temperature: 0,
        max_tokens: -1,
        rubric: defaultRubric,
      };
    case 'synthesis':
      return {
        model: 'openai/gpt-5-mini',
        system_prompt: 'You synthesize the strongest answer from all responses.',
        prompt: 'Question: {{prompt}}\n\nResponses:\n{{responses}}\n\nProduce one answer.',
        temperature: 0,
        max_tokens: -1,
      };
    case 'peer_matrix':
      return {
        rubric_mode: 'static',
        eval_system_prompt: 'You score peer responses critically.',
        eval_prompt: 'Question: {{question}}\n\nResponse: {{response}}\n\n{{rubric}}',
        temperature: 0,
        max_tokens: -1,
        normalization: 'none',
        max_parallel: 2,
        rubric: defaultRubric,
      };
  }
}

function builderAggregationMacroPayload(method: AggregationMethod) {
  const nodes: Node<NodeData>[] = [
    {
      id: 'input-1',
      type: 'input',
      position: { x: -300, y: 40 },
      data: {
        type: 'input',
        label: 'Input',
        config: { name: 'input', schema: { user_prompt: { type: 'text', required: true } } },
      },
    },
    agentNode('agent-a', 'openai/gpt-4o-mini'),
    agentNode('agent-b', 'anthropic/claude-sonnet-4.5'),
    {
      id: `agg-${method}`,
      type: 'aggregation',
      position: { x: 400, y: 40 },
      data: {
        type: 'aggregation',
        label: `${method} aggregation`,
        config: {
          name: `${method} aggregation`,
          aggregationMethod: method,
          aggregationConfig: aggregationConfigFor(method),
          retryPolicy: { max_attempts: 1 },
        },
      },
    },
    resultMarker(`result-${method}`),
  ];
  const edges: Edge[] = [
    { id: 'e1', source: 'input-1', target: 'agent-a' },
    { id: 'e2', source: 'input-1', target: 'agent-b' },
    { id: 'e3', source: 'agent-a', target: `agg-${method}` },
    { id: 'e4', source: 'agent-b', target: `agg-${method}` },
    { id: 'e5', source: `agg-${method}`, target: `result-${method}` },
  ];
  return flowToWorkflow(nodes, edges, `builder-aggregation-${method}`, '', { user_prompt: SAMPLE_PROMPT });
}

// Each entry is a representative canvas workflow run through one of the real
// converters. Cover both converters and the node types they emit.
const PAYLOADS: Record<string, () => unknown> = {
  // Ensemble path: multi-agent fan-out into a synthesis result node.
  'ensemble-synthesis': () => {
    const nodes = [
      agentNode('agent-a', 'xiaomi/mimo-v2-flash'),
      agentNode('agent-b', 'moonshotai/kimi-k2.7-code'),
      syntheticResult(),
    ];
    const file: WorkflowFileFormat = {
      version: '1.0.0',
      id: 'reasoning-informed-captain-synthesis',
      name: 'ensemble-query',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
      nodes,
      edges: [],
    };
    return buildExecutionRequest(file, SAMPLE_PROMPT);
  },

  // Builder path: agent + collect result via the React Flow converter.
  'builder-collect': () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: {
          type: 'input',
          label: 'Input',
          config: { name: 'input', schema: { user_prompt: { type: 'text', required: true } } },
        },
      },
      agentNode('agent-1', 'openai/gpt-4o-mini'),
      {
        id: 'result-1',
        type: 'result',
        position: { x: 800, y: 0 },
        data: {
          type: 'result',
          label: 'Result',
          config: { name: 'final_answer', outputFormat: 'text', aggregationMethod: 'collect' },
        },
      },
    ];
    const edges: Edge[] = [
      { id: 'e1', source: 'input-1', target: 'agent-1' },
      { id: 'e2', source: 'agent-1', target: 'result-1' },
    ];
    return flowToWorkflow(nodes, edges, 'builder-collect', '', { user_prompt: SAMPLE_PROMPT });
  },
  'builder-aggregation-collect': () => builderAggregationMacroPayload('collect'),
  'builder-aggregation-majority_vote': () => builderAggregationMacroPayload('majority_vote'),
  'builder-aggregation-debate_decide': () => builderAggregationMacroPayload('debate_decide'),
  'builder-aggregation-judge': () => builderAggregationMacroPayload('judge'),
  'builder-aggregation-scoring': () => builderAggregationMacroPayload('scoring'),
  'builder-aggregation-synthesis': () => builderAggregationMacroPayload('synthesis'),
  'builder-aggregation-peer_matrix': () => builderAggregationMacroPayload('peer_matrix'),
};

describe('submit payload contract (frontend↔backend)', () => {
  if (!existsSync(FIXTURE_DIR)) {
    mkdirSync(FIXTURE_DIR, { recursive: true });
  }

  for (const [name, build] of Object.entries(PAYLOADS)) {
    it(`pins ${name} submit payload as a validation fixture`, () => {
      // Deterministic, JSON-clean serialization so the Go side reads exactly
      // what the converter produced (drops `undefined`, stable key order via
      // the converter output shape).
      const generated = `${JSON.stringify(build(), null, 2)}\n`;
      const file = join(FIXTURE_DIR, `${name}.json`);

      if (process.env.UPDATE_SUBMIT_CONTRACT || !existsSync(file)) {
        writeFileSync(file, generated);
      }

      const committed = readFileSync(file, 'utf8');
      expect(generated).toBe(committed);
    });
  }
});
