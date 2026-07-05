import { describe, expect, it } from 'bun:test';
import type { Node } from '@xyflow/react';
import type { NodeData } from '../types/workflow';
import type { WorkflowFileFormat } from '../types/workflowFile';
import { buildExecutionRequest } from './ensembleSync';

// Regression: the backend rejects nodes that omit retry_policy/temperature/
// max_tokens/timeout_seconds (strict workflow defaults — no implicit defaults).
// buildExecutionRequest used to drop these, producing 400 INVALID_WORKFLOW on
// every Ensemble submit.
function fileWith(nodes: Node<NodeData>[]): WorkflowFileFormat {
  return {
    version: '1.0.0',
    name: 'test',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    nodes,
    edges: [],
  };
}

describe('buildExecutionRequest', () => {
  it('emits explicit temperature/max_tokens/timeout_seconds/retry_policy for agent nodes', () => {
    const file = fileWith([
      {
        id: 'agent-mimo',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent',
          label: 'MiMo',
          config: {
            model: 'xiaomi/mimo-v2-flash',
            systemPrompt: 'solve {{user_prompt}}',
            temperature: 0.2,
            maxTokens: -1,
            timeoutSeconds: 180,
            retryPolicy: { max_attempts: 5, backoff_ms: 1000 },
          },
        },
      },
      {
        id: 'result-synthesis',
        type: 'result',
        position: { x: 200, y: 0 },
        data: {
          type: 'result',
          label: 'Synthesis',
          config: {
            name: 'synthesized_response',
            aggregationMethod: 'synthesis',
            aggregationConfig: {
              model: 'minimax/minimax-m2.5',
              system_prompt: 'synthesize',
              prompt: '{{responses}}',
              temperature: 0,
              max_tokens: -1,
            },
            retryPolicy: { max_attempts: 1 },
          },
        },
      },
    ]);

    const req = buildExecutionRequest(file, 'what is 2+2?');
    const agent = req.nodes.find((n) => n.id === 'agent-mimo');
    const result = req.nodes.find((n) => n.id === 'result-synthesis');

    // Seed values are forwarded verbatim (note temperature 0 and max_tokens -1
    // must survive — they are falsy but valid).
    expect(agent?.temperature).toBe(0.2);
    expect(agent?.max_tokens).toBe(-1);
    expect(agent?.timeout_seconds).toBe(180);
    expect(agent?.retry_policy?.max_attempts).toBe(5);

    // Every node (result included) must carry an explicit retry_policy.
    expect(result?.retry_policy?.max_attempts).toBe(1);
    expect(result?.aggregation_method).toBe('synthesis');
  });

  it('falls back to explicit defaults when an agent node omits the fields', () => {
    const file = fileWith([
      {
        id: 'agent-bare',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent',
          label: 'Bare',
          config: { model: 'openai/gpt-4o-mini', systemPrompt: 'hi' },
        },
      },
    ]);

    const agent = buildExecutionRequest(file, 'q').nodes.find((n) => n.id === 'agent-bare');

    // Defaults must be present and valid for the backend validator (temperature
    // non-null, max_tokens != 0, timeout > 0, retry_policy with max_attempts >= 1).
    expect(typeof agent?.temperature).toBe('number');
    expect(agent?.max_tokens).not.toBe(0);
    expect(agent?.timeout_seconds).toBeGreaterThan(0);
    expect(agent?.retry_policy?.max_attempts).toBeGreaterThanOrEqual(1);
  });

  it('lowers builder aggregation macro nodes instead of falling back to result collect', () => {
    const file = {
      ...fileWith([
        {
          id: 'agent-a',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent A',
            config: {
              model: 'xiaomi/mimo-v2-flash',
              systemPrompt: 'solve {{user_prompt}}',
              maxTokens: -1,
              timeoutSeconds: 180,
            },
          },
        },
        {
          id: 'aggregation-synthesis',
          type: 'aggregation',
          position: { x: 200, y: 0 },
          data: {
            type: 'aggregation',
            label: 'Synthesis Aggregation',
            config: {
              name: 'Synthesis Aggregation',
              aggregationMethod: 'synthesis',
              aggregationConfig: {
                model: 'minimax/minimax-m2.5',
                system_prompt: 'synthesize',
                prompt: '{{responses}}',
                temperature: 0,
                max_tokens: -1,
              },
              timeoutSeconds: 120,
              retryPolicy: { max_attempts: 1 },
            },
          },
        },
        {
          id: 'result-synthesis',
          type: 'result',
          position: { x: 520, y: 0 },
          data: {
            type: 'result',
            label: 'Synthesis Result',
            config: {
              name: 'synthesized_response',
              outputFormat: 'text',
            },
          },
        },
      ]),
      edges: [{ id: 'edge-aggregation-result', source: 'aggregation-synthesis', target: 'result-synthesis' }],
    };

    const req = buildExecutionRequest(file, 'answer exactly OK');
    const aggregation = req.nodes.find((n) => n.id === 'aggregation-synthesis');
    const presentationResult = req.nodes.find((n) => n.id === 'result-synthesis');

    expect(aggregation?.type).toBe('result');
    expect(aggregation?.aggregation_method).toBe('synthesis');
    expect(aggregation?.output_name).toBe('synthesized_response');
    expect(aggregation?.retry_policy?.max_attempts).toBe(1);
    expect(aggregation?.timeout_seconds).toBe(120);
    expect(aggregation?.metadata?.input_ids).toEqual(['agent-a']);
    expect(presentationResult).toBeUndefined();
    expect(req.edges).toEqual([{ source: 'agent-a', target: 'aggregation-synthesis' }]);
  });
});
