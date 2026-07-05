import { describe, expect, it } from 'bun:test';
import type { Edge, Node } from '@xyflow/react';
import { DEFAULT_MODEL, type NodeData, type Workflow } from '../types/workflow';
import {
  extractInputSchema,
  flowToWorkflow,
  flowToWorkflowFile,
  runtimeWorkflowToWorkflowFile,
  workflowFileToFlow,
} from './workflowConverter';
import { validateWorkflow } from './workflowValidation';

describe('workflowConverter', () => {
  describe('extractInputSchema', () => {
    it('should extract schema from input node', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: {
              name: 'userInput',
              schema: {
                bookname: { type: 'text', label: 'Book Name', required: true },
                author: { type: 'text', label: 'Author', required: false },
              },
            },
          },
        },
      ];

      const schema = extractInputSchema(nodes);
      expect(schema).toEqual({
        bookname: { type: 'text', label: 'Book Name', required: true },
        author: { type: 'text', label: 'Author', required: false },
      });
    });

    it('should return empty object when no input nodes', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Test' },
          },
        },
      ];

      const schema = extractInputSchema(nodes);
      expect(schema).toEqual({});
    });

    it('should handle input node with no schema', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: { name: 'input1' },
          },
        },
      ];

      const schema = extractInputSchema(nodes);
      expect(schema).toEqual({});
    });
  });

  describe('flowToWorkflow - Input Data Flow', () => {
    it('should pass input values to workflow context', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: {
              name: 'userInput',
              schema: {
                bookname: { type: 'text', label: 'Book Name', required: true },
              },
            },
          },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: {
              model: 'openai/gpt-4o-mini',
              systemPrompt: 'Summarize this book',
            },
          },
        },
      ];

      const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'agent-1' }];

      const inputValues = { bookname: 'The Great Gatsby' };
      const workflow = flowToWorkflow(nodes, edges, 'Test', '', inputValues);

      expect(workflow.context).toEqual({ bookname: 'The Great Gatsby' });
    });

    it('should auto-inject input variables into agent prompt when no variables present', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: {
              name: 'userInput',
              schema: {
                bookname: { type: 'text', label: 'Book Name', required: true },
              },
            },
          },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: {
              model: 'openai/gpt-4o-mini',
              systemPrompt: 'You summarize books like a professional reviewer.',
            },
          },
        },
      ];

      const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'agent-1' }];

      const inputValues = { bookname: 'The Great Gatsby' };
      const workflow = flowToWorkflow(nodes, edges, 'Test', '', inputValues);

      expect(workflow.nodes).toHaveLength(1);
      expect(workflow.nodes[0].prompt).toContain('Input data:');
      expect(workflow.nodes[0].prompt).toContain('{{bookname}}');
      expect(workflow.nodes[0].prompt).toContain('You summarize books like a professional reviewer.');
    });

    it('should not auto-inject when agent prompt already has variable references', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: {
              name: 'userInput',
              schema: {
                bookname: { type: 'text', label: 'Book Name', required: true },
              },
            },
          },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: {
              model: 'openai/gpt-4o-mini',
              systemPrompt: 'Summarize the book {{bookname}} professionally.',
            },
          },
        },
      ];

      const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'agent-1' }];

      const inputValues = { bookname: 'The Great Gatsby' };
      const workflow = flowToWorkflow(nodes, edges, 'Test', '', inputValues);

      expect(workflow.nodes).toHaveLength(1);
      expect(workflow.nodes[0].prompt).toBe('Summarize the book {{bookname}} professionally.');
      expect(workflow.nodes[0].prompt).not.toContain('Input data:');
    });

    it('should handle multiple input fields', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: {
              name: 'userInput',
              schema: {
                bookname: { type: 'text', label: 'Book Name', required: true },
                author: { type: 'text', label: 'Author', required: true },
                year: { type: 'text', label: 'Year', required: false },
              },
            },
          },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: {
              model: 'openai/gpt-4o-mini',
              systemPrompt: 'Analyze this book.',
            },
          },
        },
      ];

      const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'agent-1' }];

      const inputValues = {
        bookname: 'The Great Gatsby',
        author: 'F. Scott Fitzgerald',
        year: '1925',
      };
      const workflow = flowToWorkflow(nodes, edges, 'Test', '', inputValues);

      expect(workflow.context).toEqual(inputValues);
      expect(workflow.nodes[0].prompt).toContain('{{bookname}}');
      expect(workflow.nodes[0].prompt).toContain('{{author}}');
      expect(workflow.nodes[0].prompt).toContain('{{year}}');
    });

    it('should handle workflow with no input node - agent only', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: {
              model: 'openai/gpt-4o-mini',
              systemPrompt: 'Generate a random story.',
            },
          },
        },
        {
          id: 'output-1',
          type: 'result',
          position: { x: 200, y: 0 },
          data: {
            type: 'result',
            label: 'Output',
            config: {},
          },
        },
      ];

      const edges: Edge[] = [{ id: 'e1', source: 'agent-1', target: 'output-1' }];

      const workflow = flowToWorkflow(nodes, edges, 'Test', '', {});

      expect(workflow.context).toEqual({});
      // agent-1 creates a prompt node, output-1 creates an output node
      expect(workflow.nodes).toHaveLength(2);
      expect(workflow.nodes[0].type).toBe('prompt');
      expect(workflow.nodes[1].type).toBe('result');
    });

    it('should preserve agent configuration (temperature, maxTokens)', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: { name: 'input1', schema: { query: { type: 'text', required: true } } },
          },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: {
              model: 'openai/gpt-4o-mini',
              systemPrompt: 'Answer {{query}}',
              temperature: 0.5,
              maxTokens: 500,
            },
          },
        },
      ];

      const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'agent-1' }];

      const workflow = flowToWorkflow(nodes, edges, 'Test', '', { query: 'What is AI?' });

      expect(workflow.nodes[0].temperature).toBe(0.5);
      expect(workflow.nodes[0].max_tokens).toBe(500);
      expect(workflow.nodes[0].model).toBe('openai/gpt-4o-mini');
    });

    it('uses the platform default model when builder agent nodes omit a model', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { systemPrompt: 'Answer directly.' },
          },
        },
      ];

      const workflow = flowToWorkflow(nodes, [], 'Test', '', {});

      expect(workflow.nodes[0].model).toBe(DEFAULT_MODEL);
    });
  });

  describe('flowToWorkflow - Basic Functionality', () => {
    it('should convert deterministic operation nodes without retry policy', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer.' },
          },
        },
        {
          id: 'vote-counter',
          type: 'operation',
          position: { x: 220, y: 0 },
          data: {
            type: 'operation',
            label: 'Vote Counter',
            config: {
              name: 'Count votes',
              operationType: 'count_votes',
              operationConfig: { answers: '{{answers}}' },
            },
          },
        },
        {
          id: 'result-1',
          type: 'result',
          position: { x: 440, y: 0 },
          data: { type: 'result', label: 'Result', config: { name: 'answer' } },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'agent-1', target: 'vote-counter' },
        { id: 'e2', source: 'vote-counter', target: 'result-1' },
      ];

      const workflow = flowToWorkflow(nodes, edges);
      const operation = workflow.nodes.find((node) => node.id === 'vote-counter');

      expect(operation).toMatchObject({
        id: 'vote-counter',
        type: 'operation',
        operation_type: 'count_votes',
        operation_config: { answers: '{{answers}}' },
        metadata: {
          input_ids: ['agent-1'],
          name: 'Count votes',
        },
      });
      expect(operation?.retry_policy).toBeUndefined();
      expect(workflow.edges).toContainEqual({ id: 'e2', source: 'vote-counter', target: 'result-1' });
    });

    it('should convert simple workflow with input->agent->output', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: { type: 'input', label: 'Input', config: { name: 'start' } },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Test prompt' },
          },
        },
        {
          id: 'output-1',
          type: 'result',
          position: { x: 400, y: 0 },
          data: { type: 'result', label: 'Output', config: {} },
        },
      ];

      const edges: Edge[] = [
        { id: 'e1', source: 'input-1', target: 'agent-1' },
        { id: 'e2', source: 'agent-1', target: 'output-1' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      expect(workflow.nodes).toHaveLength(2);
      expect(workflow.nodes[0].type).toBe('prompt');
      expect(workflow.nodes[0].id).toBe('agent-1');
      expect(workflow.nodes[1].type).toBe('result');
      expect(workflow.nodes[1].id).toBe('output-1');
    });

    it('should handle conditional nodes', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: { type: 'input', label: 'Input', config: { name: 'start' } },
        },
        {
          id: 'cond-1',
          type: 'conditional',
          position: { x: 200, y: 0 },
          data: {
            type: 'conditional',
            label: 'Condition',
            config: { condition: 'value contains yes' },
          },
        },
        {
          id: 'agent-true',
          type: 'agent',
          position: { x: 400, y: -100 },
          data: {
            type: 'agent',
            label: 'True Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Yes response' },
          },
        },
        {
          id: 'agent-false',
          type: 'agent',
          position: { x: 400, y: 100 },
          data: {
            type: 'agent',
            label: 'False Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'No response' },
          },
        },
      ];

      const edges: Edge[] = [
        { id: 'e1', source: 'input-1', target: 'cond-1' },
        { id: 'e2', source: 'cond-1', target: 'agent-true', sourceHandle: 'true' },
        { id: 'e3', source: 'cond-1', target: 'agent-false', sourceHandle: 'false' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      // Conditional nodes not yet supported, so both agents are included as separate nodes
      expect(workflow.nodes).toHaveLength(2);
      expect(workflow.nodes.some((s) => s.id === 'agent-true')).toBe(true);
      expect(workflow.nodes.some((s) => s.id === 'agent-false')).toBe(true);
    });

    it('reconstructs forked aggregation conditional branches on submit', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agg--repair-selection',
          type: 'conditional',
          position: { x: 0, y: 0 },
          data: {
            type: 'conditional',
            label: 'Repair Selection',
            config: {
              condition: 'agg--parse-selection is_empty',
              aggregationInternalState: 'forked',
              aggregationAnchorId: 'agg',
              sourceLocked: false,
            },
          },
        },
        {
          id: 'agg--repair-selection--true',
          type: 'agent',
          position: { x: 300, y: -80 },
          data: {
            type: 'agent',
            label: 'Repair Call',
            config: {
              model: 'openai/gpt-5-mini',
              systemPrompt: '',
              userPrompt: 'Return the winner',
              aggregationInternalState: 'forked',
              aggregationAnchorId: 'agg',
              aggregationBranch: 'true',
              aggregationBranchParentId: 'agg--repair-selection',
              sourceLocked: false,
            },
          },
        },
        {
          id: 'agg--select-winner',
          type: 'operation',
          position: { x: 600, y: 0 },
          data: {
            type: 'operation',
            label: 'Select Winner',
            config: {
              operationType: 'select_winner',
              operationConfig: { fallback_selection: '{{agg--repair-selection}}' },
              aggregationInternalState: 'forked',
              aggregationAnchorId: 'agg',
              sourceLocked: false,
            },
          },
        },
      ];
      const edges: Edge[] = [
        {
          id: 'repair-true',
          source: 'agg--repair-selection',
          target: 'agg--repair-selection--true',
          sourceHandle: 'true',
        },
        { id: 'repair-select', source: 'agg--repair-selection', target: 'agg--select-winner' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      const conditional = workflow.nodes.find((node) => node.id === 'agg--repair-selection');
      expect(conditional?.type).toBe('conditional');
      expect(conditional?.true_branch?.type).toBe('prompt');
      expect(conditional?.true_branch?.id).toBe('agg--repair-selection--true');
      expect(workflow.nodes.some((node) => node.id === 'agg--repair-selection--true')).toBe(false);
      expect(workflow.edges).toEqual([
        { id: 'repair-select', source: 'agg--repair-selection', target: 'agg--select-winner' },
      ]);
    });

    it('should add required token defaults for judge aggregation', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Respond.' },
          },
        },
        {
          id: 'result-1',
          type: 'result',
          position: { x: 200, y: 0 },
          data: {
            type: 'result',
            label: 'Result',
            config: {
              aggregationMethod: 'judge',
              aggregationConfig: {},
            },
          },
        },
      ];
      const edges: Edge[] = [{ id: 'e1', source: 'agent-1', target: 'result-1' }];

      const workflow = flowToWorkflow(nodes, edges);
      const resultNode = workflow.nodes.find((node) => node.id === 'result-1');

      expect(resultNode?.aggregation_method).toBe('judge');
      expect(resultNode?.aggregation_config).toMatchObject({
        judge_model: DEFAULT_MODEL,
        max_tokens: -1,
        repair_max_tokens: 256,
      });
    });

    it('defaults model fields for model-bearing aggregation methods', () => {
      const cases: Array<{
        method: 'synthesis' | 'judge' | 'scoring' | 'debate_decide';
        field: 'model' | 'judge_model' | 'scoring_model';
      }> = [
        { method: 'synthesis', field: 'model' },
        { method: 'judge', field: 'judge_model' },
        { method: 'scoring', field: 'scoring_model' },
        { method: 'debate_decide', field: 'judge_model' },
      ];

      for (const { method, field } of cases) {
        const nodes: Node<NodeData>[] = [
          {
            id: 'agent-1',
            type: 'agent',
            position: { x: 0, y: 0 },
            data: {
              type: 'agent',
              label: 'Agent',
              config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Respond.' },
            },
          },
          {
            id: 'result-1',
            type: 'result',
            position: { x: 200, y: 0 },
            data: {
              type: 'result',
              label: 'Result',
              config: {
                aggregationMethod: method,
                aggregationConfig: {},
              },
            },
          },
        ];
        const edges: Edge[] = [{ id: 'e1', source: 'agent-1', target: 'result-1' }];

        const workflow = flowToWorkflow(nodes, edges);
        const resultNode = workflow.nodes.find((node) => node.id === 'result-1');
        const aggregationConfig = resultNode?.aggregation_config as Record<string, unknown> | undefined;

        expect(aggregationConfig?.[field]).toBe(DEFAULT_MODEL);
      }
    });

    it('should preserve explicit token config for debate_decide aggregation', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer A.' },
          },
        },
        {
          id: 'agent-2',
          type: 'agent',
          position: { x: 0, y: 100 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer B.' },
          },
        },
        {
          id: 'result-1',
          type: 'result',
          position: { x: 300, y: 50 },
          data: {
            type: 'result',
            label: 'Result',
            config: {
              aggregationMethod: 'debate_decide',
              aggregationConfig: {
                max_tokens: 900,
                repair_max_tokens: 333,
              },
            },
          },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'agent-1', target: 'result-1' },
        { id: 'e2', source: 'agent-2', target: 'result-1' },
      ];

      const workflow = flowToWorkflow(nodes, edges);
      const resultNode = workflow.nodes.find((node) => node.id === 'result-1');

      expect(resultNode?.aggregation_method).toBe('debate_decide');
      expect(resultNode?.aggregation_config).toMatchObject({
        max_tokens: 900,
        repair_max_tokens: 333,
      });
    });

    it('should compile aggregation macro nodes into backend result nodes anchored on the aggregation id', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent 1',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer.' },
          },
        },
        {
          id: 'agent-2',
          type: 'agent',
          position: { x: 0, y: 100 },
          data: {
            type: 'agent',
            label: 'Agent 2',
            config: { model: 'anthropic/claude-sonnet-4.5', systemPrompt: 'Answer.' },
          },
        },
        {
          id: 'agg-judge',
          type: 'aggregation',
          position: { x: 250, y: 50 },
          data: {
            type: 'aggregation',
            label: 'Judge Aggregation',
            config: {
              aggregationMethod: 'judge',
              aggregationConfig: {
                judge_model: 'openai/gpt-5-mini',
                system_prompt: 'Judge.',
                prompt: '{{responses}}',
                temperature: 0,
              },
            },
          },
        },
        {
          id: 'final-result',
          type: 'result',
          position: { x: 500, y: 50 },
          data: {
            type: 'result',
            label: 'Final',
            config: {
              name: 'final_answer',
              outputFormat: 'text',
            },
          },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'agent-1', target: 'agg-judge' },
        { id: 'e2', source: 'agent-2', target: 'agg-judge' },
        { id: 'e3', source: 'agg-judge', target: 'final-result' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      const aggregationRuntimeNode = workflow.nodes.find((node) => node.id === 'agg-judge');
      expect(aggregationRuntimeNode?.type).toBe('result');
      expect(aggregationRuntimeNode?.output_name).toBe('final_answer');
      expect(aggregationRuntimeNode?.aggregation_method).toBe('judge');
      expect(aggregationRuntimeNode?.aggregation_config).toMatchObject({
        judge_model: 'openai/gpt-5-mini',
        max_tokens: -1,
        repair_max_tokens: 256,
      });
      expect(aggregationRuntimeNode?.metadata?.input_ids).toEqual(['agent-1', 'agent-2']);
      expect(aggregationRuntimeNode?.metadata?.aggregation_anchor_id).toBe('agg-judge');
      expect(aggregationRuntimeNode?.metadata?.presentation_result_id).toBe('final-result');
      expect(workflow.nodes.some((node) => node.id === 'final-result')).toBe(false);
      expect(workflow.edges).toEqual([
        { id: 'e1', source: 'agent-1', target: 'agg-judge' },
        { id: 'e2', source: 'agent-2', target: 'agg-judge' },
      ]);
    });

    it('should normalize majority vote tie_breaker to backend tie_breaker_method when compiling macros', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-a',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent A',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer A.' },
          },
        },
        {
          id: 'agent-b',
          type: 'agent',
          position: { x: 0, y: 100 },
          data: {
            type: 'agent',
            label: 'Agent B',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer B.' },
          },
        },
        {
          id: 'agg-vote',
          type: 'aggregation',
          position: { x: 250, y: 50 },
          data: {
            type: 'aggregation',
            label: 'Vote',
            config: {
              aggregationMethod: 'majority_vote',
              aggregationConfig: {
                extraction_strategy: 'regex',
                extraction_pattern: 'Answer:\\s*([A-D])',
                tie_breaker: 'first',
              },
            },
          },
        },
        {
          id: 'result-vote',
          type: 'result',
          position: { x: 500, y: 50 },
          data: {
            type: 'result',
            label: 'Vote Result',
            config: { name: 'voted_answer' },
          },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'agent-a', target: 'agg-vote' },
        { id: 'e2', source: 'agent-b', target: 'agg-vote' },
        { id: 'e3', source: 'agg-vote', target: 'result-vote' },
      ];

      const workflow = flowToWorkflow(nodes, edges);
      const resultNode = workflow.nodes.find((node) => node.id === 'agg-vote');

      expect(resultNode?.aggregation_config).toMatchObject({
        extraction_strategy: 'regex',
        extraction_pattern: 'Answer:\\s*([A-D])',
        tie_breaker_method: 'first',
      });
      expect(resultNode?.aggregation_config).not.toHaveProperty('tie_breaker');
    });

    it('should serialize aggregation macros with aggregationWorkflowId as workflow refs', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-a',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent A',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer A.' },
          },
        },
        {
          id: 'agent-b',
          type: 'agent',
          position: { x: 0, y: 100 },
          data: {
            type: 'agent',
            label: 'Agent B',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer B.' },
          },
        },
        {
          id: 'agg-vote',
          type: 'aggregation',
          position: { x: 250, y: 50 },
          data: {
            type: 'aggregation',
            label: 'Vote',
            config: {
              aggregationMethod: 'majority_vote',
              aggregationWorkflowId: 'aggregation-majority-vote',
              aggregationConfig: {
                extraction_strategy: 'regex',
                extraction_pattern: 'Answer:\\s*([A-D])',
                tie_breaker: 'first',
              },
            },
          },
        },
        {
          id: 'result-vote',
          type: 'result',
          position: { x: 500, y: 50 },
          data: {
            type: 'result',
            label: 'Vote Result',
            config: { name: 'voted_answer' },
          },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'agent-a', target: 'agg-vote' },
        { id: 'e2', source: 'agent-b', target: 'agg-vote' },
        { id: 'e3', source: 'agg-vote', target: 'result-vote' },
      ];

      const workflow = flowToWorkflow(nodes, edges);
      const aggregationNode = workflow.nodes.find((node) => node.id === 'agg-vote');

      expect(aggregationNode?.type).toBe('workflow_ref');
      expect(aggregationNode?.workflow_ref_id).toBe('aggregation-majority-vote');
      expect(aggregationNode?.aggregation_method).toBe('majority_vote');
      expect(aggregationNode?.aggregation_config).toMatchObject({
        extraction_strategy: 'regex',
        extraction_pattern: 'Answer:\\s*([A-D])',
        tie_breaker_method: 'first',
      });
      expect(aggregationNode?.aggregation_config).not.toHaveProperty('tie_breaker');
      expect(aggregationNode?.metadata?.input_ids).toEqual(['agent-a', 'agent-b']);
      expect(aggregationNode?.metadata?.presentation_result_id).toBe('result-vote');
      expect(workflow.nodes.some((node) => node.id === 'result-vote')).toBe(false);
    });

    it('should pass known aggregation workflow refs through for backend validation', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-a',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent A',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer A.' },
          },
        },
        {
          id: 'agent-b',
          type: 'agent',
          position: { x: 0, y: 100 },
          data: {
            type: 'agent',
            label: 'Agent B',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer B.' },
          },
        },
        {
          id: 'agg-judge',
          type: 'aggregation',
          position: { x: 250, y: 50 },
          data: {
            type: 'aggregation',
            label: 'Judge',
            config: {
              aggregationMethod: 'judge',
              aggregationWorkflowId: 'aggregation-scoring',
              benchmarkOutputPackaging: true,
              aggregationConfig: {
                judge_model: 'openai/gpt-5-mini',
                system_prompt: 'Judge.',
                prompt: '{{responses}}',
              },
            },
          },
        },
        {
          id: 'result-judge',
          type: 'result',
          position: { x: 500, y: 50 },
          data: {
            type: 'result',
            label: 'Judge Result',
            config: { name: 'judged_answer' },
          },
        },
      ];
      const edges: Edge[] = [
        { id: 'e1', source: 'agent-a', target: 'agg-judge' },
        { id: 'e2', source: 'agent-b', target: 'agg-judge' },
        { id: 'e3', source: 'agg-judge', target: 'result-judge' },
      ];

      const workflow = flowToWorkflow(nodes, edges);
      const aggregationNode = workflow.nodes.find((node) => node.id === 'agg-judge');

      expect(aggregationNode?.type).toBe('workflow_ref');
      expect(aggregationNode?.workflow_ref_id).toBe('aggregation-scoring');
      expect(aggregationNode?.aggregation_method).toBe('judge');
      expect(aggregationNode?.metadata?.aggregation_method).toBe('judge');
      expect(aggregationNode?.metadata?.benchmark_output_packaging).toBe(true);
    });
  });

  describe('flowToWorkflowFile and workflowFileToFlow', () => {
    it('should preserve all node data in file format', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 100, y: 200 },
          data: {
            type: 'input',
            label: 'My Input',
            config: {
              name: 'userInput',
              description: 'User input data',
              schema: { field1: { type: 'text', required: true } },
            },
          },
        },
      ];

      const edges: Edge[] = [];

      const file = flowToWorkflowFile(nodes, edges, 'Test Workflow', 'Test description');

      expect(file.version).toBe('1.0.0');
      expect(file.name).toBe('Test Workflow');
      expect(file.description).toBe('Test description');
      expect(file.nodes).toEqual(nodes);
      expect(file.edges).toEqual(edges);
      expect(file.id).toBeUndefined(); // Backend assigns UUID
      expect(file.created_at).toBeDefined();
      expect(file.updated_at).toBeDefined();
    });

    it('should convert file format back to flow', () => {
      const originalNodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 100, y: 200 },
          data: {
            type: 'input',
            label: 'Input',
            config: { name: 'input1' },
          },
        },
      ];

      const originalEdges: Edge[] = [];

      const file = flowToWorkflowFile(originalNodes, originalEdges, 'Test');
      const { nodes, edges } = workflowFileToFlow(file);

      expect(nodes).toEqual(originalNodes);
      expect(edges).toEqual(originalEdges);
    });

    it('should exclude read-only expanded aggregation internals from saved workflow files', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agg-judge',
          type: 'aggregation',
          position: { x: 0, y: 0 },
          data: {
            type: 'aggregation',
            label: 'Judge',
            config: {
              aggregationMethod: 'judge',
              aggregationWorkflowId: 'aggregation-judge',
            },
          },
        },
        {
          id: 'agg-judge--op-format-candidates',
          type: 'operation',
          position: { x: 180, y: 0 },
          data: {
            type: 'operation',
            label: 'Format Candidates',
            config: {
              aggregationInternalState: 'expanded',
              sourceLocked: true,
              aggregationAnchorId: 'agg-judge',
              operationType: 'format_candidates',
              operationConfig: { candidates: '{{candidates}}' },
            },
          },
        },
      ];
      const edges: Edge[] = [
        { id: 'candidate-agg', source: 'agent-a', target: 'agg-judge' },
        { id: 'agg-judge--edge-format', source: 'agg-judge', target: 'agg-judge--op-format-candidates' },
      ];

      const file = flowToWorkflowFile(nodes, edges, 'Expanded');

      expect(file.nodes.map((node) => node.id)).toEqual(['agg-judge']);
      expect(file.edges).toEqual([{ id: 'candidate-agg', source: 'agent-a', target: 'agg-judge' }]);
    });
  });

  describe('Edge Cases', () => {
    it('should handle empty workflow', () => {
      const workflow = flowToWorkflow([], [], 'Empty');
      expect(workflow.nodes).toHaveLength(0);
      expect(workflow.context).toEqual({});
    });

    it('should handle workflow with only result node', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'output-1',
          type: 'result',
          position: { x: 0, y: 0 },
          data: { type: 'result', label: 'Output', config: {} },
        },
      ];

      const workflow = flowToWorkflow(nodes, []);
      expect(workflow.nodes).toHaveLength(1);
      expect(workflow.nodes[0].type).toBe('result');
    });

    it('should exclude read-only expanded aggregation internals from runtime submission', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'agent-a',
          type: 'agent',
          position: { x: 0, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent A',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer A.' },
          },
        },
        {
          id: 'agg-judge',
          type: 'aggregation',
          position: { x: 200, y: 0 },
          data: {
            type: 'aggregation',
            label: 'Judge',
            config: {
              aggregationMethod: 'judge',
              aggregationWorkflowId: 'aggregation-judge',
            },
          },
        },
        {
          id: 'agg-judge--op-format-candidates',
          type: 'operation',
          position: { x: 360, y: 0 },
          data: {
            type: 'operation',
            label: 'Format Candidates',
            config: {
              aggregationInternalState: 'expanded',
              sourceLocked: true,
              aggregationAnchorId: 'agg-judge',
              operationType: 'format_candidates',
              operationConfig: { candidates: '{{candidates}}' },
            },
          },
        },
      ];
      const edges: Edge[] = [
        { id: 'agent-agg', source: 'agent-a', target: 'agg-judge' },
        { id: 'agg-expanded', source: 'agg-judge', target: 'agg-judge--op-format-candidates' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      expect(workflow.nodes.map((node) => node.id)).toEqual(['agent-a', 'agg-judge']);
      expect(workflow.nodes.find((node) => node.id === 'agg-judge')?.type).toBe('workflow_ref');
      expect(workflow.edges).toEqual([{ id: 'agent-agg', source: 'agent-a', target: 'agg-judge' }]);
    });

    it('should handle input values with undefined inputValues parameter', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: {
            type: 'input',
            label: 'Input',
            config: {
              name: 'input1',
              schema: { field: { type: 'text', required: true } },
            },
          },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Test' },
          },
        },
      ];

      const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'agent-1' }];

      const workflow = flowToWorkflow(nodes, edges);
      expect(workflow.context).toEqual({});
    });

    it('should include child_workflow nodes in output', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: { type: 'input', label: 'Input', config: { name: 'start' } },
        },
        {
          id: 'child-1',
          type: 'child_workflow',
          position: { x: 200, y: 0 },
          data: {
            type: 'child_workflow',
            label: 'Child Workflow',
            config: {
              name: 'Run Child',
              childWorkflowId: 'reasoning-informed-captain-synthesis',
              childOutputKey: 'synthesized_response',
            },
          },
        },
        {
          id: 'result-1',
          type: 'result',
          position: { x: 400, y: 0 },
          data: { type: 'result', label: 'Result', config: {} },
        },
      ];

      const edges: Edge[] = [
        { id: 'e1', source: 'input-1', target: 'child-1' },
        { id: 'e2', source: 'child-1', target: 'result-1' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      // Input skipped, child_workflow and result included
      expect(workflow.nodes).toHaveLength(2);
      expect(workflow.nodes[0].id).toBe('child-1');
      expect(workflow.nodes[0].type).toBe('child_workflow');
      expect(workflow.nodes[0].metadata?.name).toBe('Run Child');
      expect(workflow.nodes[1].id).toBe('result-1');

      // Edges should link child-1 → result-1
      expect(workflow.edges).toHaveLength(1);
      expect(workflow.edges![0].source).toBe('child-1');
      expect(workflow.edges![0].target).toBe('result-1');
    });

    it('should include workflow_ref nodes without child_workflow fields', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: { type: 'input', label: 'Input', config: { name: 'start' } },
        },
        {
          id: 'ref-1',
          type: 'workflow_ref',
          position: { x: 200, y: 0 },
          data: {
            type: 'workflow_ref',
            label: 'Workflow Ref',
            config: {
              name: 'Use Aggregation',
              workflowId: 'aggregation-synthesis',
              inputTemplate: { user_prompt: '{{user_prompt}}' },
              outputKey: 'result',
            },
          },
        },
        {
          id: 'result-1',
          type: 'result',
          position: { x: 400, y: 0 },
          data: { type: 'result', label: 'Result', config: {} },
        },
      ];

      const edges: Edge[] = [
        { id: 'e1', source: 'input-1', target: 'ref-1' },
        { id: 'e2', source: 'ref-1', target: 'result-1' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      expect(workflow.nodes).toHaveLength(2);
      expect(workflow.nodes[0].id).toBe('ref-1');
      expect(workflow.nodes[0].type).toBe('workflow_ref');
      expect(workflow.nodes[0].workflow_ref_id).toBe('aggregation-synthesis');
      expect(workflow.nodes[0].input_template).toEqual({ user_prompt: '{{user_prompt}}' });
      expect(workflow.nodes[0].output_key).toBe('result');
      expect(workflow.nodes[0].child_workflow_id).toBeUndefined();
      expect(workflow.nodes[0].child_input_template).toBeUndefined();
      expect(workflow.nodes[0].child_output_key).toBeUndefined();
      expect(workflow.edges).toHaveLength(1);
      expect(workflow.edges![0].source).toBe('ref-1');
      expect(workflow.edges![0].target).toBe('result-1');
    });

    it('should serialize workflow_ref nodes from workflowRefId config', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'ref-1',
          type: 'workflow_ref',
          position: { x: 0, y: 0 },
          data: {
            type: 'workflow_ref',
            label: 'Use Workflow',
            config: {
              workflowRefId: 'reasoning-informed-captain-synthesis',
              inputTemplate: { user_prompt: '{{user_prompt}}' },
              outputKey: 'result',
            },
          },
        },
      ];

      const workflow = flowToWorkflow(nodes, []);

      expect(workflow.nodes).toHaveLength(1);
      expect(workflow.nodes[0].type).toBe('workflow_ref');
      expect(workflow.nodes[0].workflow_ref_id).toBe('reasoning-informed-captain-synthesis');
      expect(workflow.nodes[0].input_template).toEqual({ user_prompt: '{{user_prompt}}' });
      expect(workflow.nodes[0].output_key).toBe('result');
    });

    it('should skip input/output nodes and continue to next', () => {
      const nodes: Node<NodeData>[] = [
        {
          id: 'input-1',
          type: 'input',
          position: { x: 0, y: 0 },
          data: { type: 'input', label: 'Input', config: { name: 'start' } },
        },
        {
          id: 'agent-1',
          type: 'agent',
          position: { x: 200, y: 0 },
          data: {
            type: 'agent',
            label: 'Agent',
            config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Process' },
          },
        },
        {
          id: 'output-1',
          type: 'result',
          position: { x: 400, y: 0 },
          data: { type: 'result', label: 'Output', config: {} },
        },
      ];

      const edges: Edge[] = [
        { id: 'e1', source: 'input-1', target: 'agent-1' },
        { id: 'e2', source: 'agent-1', target: 'output-1' },
      ];

      const workflow = flowToWorkflow(nodes, edges);

      // Input nodes are skipped, but output nodes create nodes
      expect(workflow.nodes).toHaveLength(2);
      expect(workflow.nodes[0].id).toBe('agent-1');
      expect(workflow.nodes[1].id).toBe('output-1');
      expect(workflow.nodes[1].type).toBe('result');
    });
  });
});

describe('workflowValidation - child_workflow', () => {
  it('should error when child_workflow has no childWorkflowId', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'child-1',
        type: 'child_workflow',
        position: { x: 0, y: 0 },
        data: { type: 'child_workflow', label: 'Child', config: {} },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    const childError = result.errors.find((e) => e.nodeId === 'child-1' && e.severity === 'error');
    expect(childError).toBeDefined();
    expect(childError!.message).toContain('no child workflow selected');
  });

  it('should warn when child_workflow has no incoming or outgoing connections', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: { type: 'input', label: 'Input', config: {} },
      },
      {
        id: 'child-1',
        type: 'child_workflow',
        position: { x: 200, y: 0 },
        data: {
          type: 'child_workflow',
          label: 'Child',
          config: { childWorkflowId: 'reasoning-informed-captain-synthesis' },
        },
      },
    ];

    // No edges — child-1 is disconnected
    const result = validateWorkflow(nodes, []);
    const warnings = result.errors.filter((e) => e.nodeId === 'child-1' && e.severity === 'warning');
    expect(warnings.length).toBeGreaterThanOrEqual(2); // no incoming + no outgoing
  });

  it('should pass validation for properly configured child_workflow', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: { type: 'input', label: 'Input', config: {} },
      },
      {
        id: 'child-1',
        type: 'child_workflow',
        position: { x: 200, y: 0 },
        data: {
          type: 'child_workflow',
          label: 'Child',
          config: { childWorkflowId: 'reasoning-informed-captain-synthesis', childOutputKey: 'result' },
        },
      },
      {
        id: 'result-1',
        type: 'result',
        position: { x: 400, y: 0 },
        data: { type: 'result', label: 'Result', config: {} },
      },
    ];

    const edges: Edge[] = [
      { id: 'e1', source: 'input-1', target: 'child-1' },
      { id: 'e2', source: 'child-1', target: 'result-1' },
    ];

    const result = validateWorkflow(nodes, edges);
    // No errors specific to child-1
    const childErrors = result.errors.filter((e) => e.nodeId === 'child-1' && e.severity === 'error');
    expect(childErrors).toHaveLength(0);
  });
});

describe('workflowValidation - workflow_ref', () => {
  it('should error when workflow_ref has no workflow selected', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'ref-1',
        type: 'workflow_ref',
        position: { x: 0, y: 0 },
        data: { type: 'workflow_ref', label: 'Use Workflow', config: {} },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    const refError = result.errors.find((e) => e.nodeId === 'ref-1' && e.severity === 'error');
    expect(refError).toBeDefined();
    expect(refError!.message).toContain('no workflow selected');
  });

  it('should pass validation when workflow_ref has workflowRefId', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'ref-1',
        type: 'workflow_ref',
        position: { x: 0, y: 0 },
        data: {
          type: 'workflow_ref',
          label: 'Use Workflow',
          config: { workflowRefId: 'reasoning-informed-captain-synthesis' },
        },
      },
    ];

    const result = validateWorkflow(nodes, []);
    const refErrors = result.errors.filter((e) => e.nodeId === 'ref-1' && e.severity === 'error');
    expect(refErrors).toHaveLength(0);
  });

  it('should warn when workflow_ref has no incoming or outgoing connections', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'ref-1',
        type: 'workflow_ref',
        position: { x: 0, y: 0 },
        data: {
          type: 'workflow_ref',
          label: 'Use Workflow',
          config: { workflowRefId: 'reasoning-informed-captain-synthesis' },
        },
      },
    ];

    const result = validateWorkflow(nodes, []);
    const warnings = result.errors.filter((e) => e.nodeId === 'ref-1' && e.severity === 'warning');
    expect(warnings.length).toBeGreaterThanOrEqual(2);
  });
});

describe('workflowValidation - aggregation', () => {
  const agentNode: Node<NodeData> = {
    id: 'agent-1',
    type: 'agent',
    position: { x: 0, y: 0 },
    data: {
      type: 'agent',
      label: 'Agent',
      config: { model: 'openai/gpt-4o-mini', systemPrompt: 'Answer.' },
    },
  };
  const aggregationNode: Node<NodeData> = {
    id: 'agg-1',
    type: 'aggregation',
    position: { x: 250, y: 0 },
    data: {
      type: 'aggregation',
      label: 'Aggregation',
      config: {
        aggregationMethod: 'collect',
        aggregationConfig: { separator: '\n---\n' },
      },
    },
  };
  const resultNode: Node<NodeData> = {
    id: 'result-1',
    type: 'result',
    position: { x: 500, y: 0 },
    data: {
      type: 'result',
      label: 'Result',
      config: { name: 'final' },
    },
  };

  it('should pass validation for agent to aggregation to result', () => {
    const result = validateWorkflow(
      [agentNode, aggregationNode, resultNode],
      [
        { id: 'e1', source: 'agent-1', target: 'agg-1' },
        { id: 'e2', source: 'agg-1', target: 'result-1' },
      ],
    );

    expect(result.errors.filter((error) => error.severity === 'error')).toEqual([]);
  });

  it('should error when an aggregation node has no incoming inputs', () => {
    const result = validateWorkflow([aggregationNode, resultNode], [{ id: 'e1', source: 'agg-1', target: 'result-1' }]);

    expect(result.errors).toContainEqual({
      nodeId: 'agg-1',
      message: 'Aggregation node "Aggregation" has no incoming inputs',
      severity: 'error',
    });
  });

  it('should error when an aggregation node does not flow to exactly one result node', () => {
    const otherResult: Node<NodeData> = {
      ...resultNode,
      id: 'result-2',
      data: { ...resultNode.data, label: 'Second Result' },
    };

    const result = validateWorkflow(
      [agentNode, aggregationNode, resultNode, otherResult],
      [
        { id: 'e1', source: 'agent-1', target: 'agg-1' },
        { id: 'e2', source: 'agg-1', target: 'result-1' },
        { id: 'e3', source: 'agg-1', target: 'result-2' },
      ],
    );

    expect(result.errors).toContainEqual({
      nodeId: 'agg-1',
      message: 'Aggregation node "Aggregation" must connect to exactly one Result node',
      severity: 'error',
    });
  });

  it('should error when a downstream result node also defines aggregation config', () => {
    const conflictingResult: Node<NodeData> = {
      ...resultNode,
      data: {
        ...resultNode.data,
        config: {
          ...resultNode.data.config,
          aggregationMethod: 'judge',
          aggregationConfig: { judge_model: 'openai/gpt-5-mini' },
        },
      },
    };

    const result = validateWorkflow(
      [agentNode, aggregationNode, conflictingResult],
      [
        { id: 'e1', source: 'agent-1', target: 'agg-1' },
        { id: 'e2', source: 'agg-1', target: 'result-1' },
      ],
    );

    expect(result.errors).toContainEqual({
      nodeId: 'result-1',
      message: 'Result node "Result" is downstream of an Aggregation node and cannot define its own aggregation method',
      severity: 'error',
    });
  });
});

describe('workflowConverter - contract_extract', () => {
  it('should convert contract_extract node with source metadata', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: { type: 'input', label: 'Input', config: { name: 'user_prompt' } },
      },
      {
        id: 'contract-1',
        type: 'contract_extract',
        position: { x: 220, y: 0 },
        data: {
          type: 'contract_extract',
          label: 'Contract Extract',
          config: {
            model: 'x-ai/grok-4.1-fast',
            systemPrompt: 'Extract one letter',
            userPrompt: 'Source: {{agent-solver}}',
            sourceVariable: 'agent-solver',
            extractionPatterns: ['^\\s*([A-Za-z])\\s*$'],
          },
        },
      },
      {
        id: 'result-1',
        type: 'result',
        position: { x: 440, y: 0 },
        data: { type: 'result', label: 'Result', config: {} },
      },
    ];

    const edges: Edge[] = [
      { id: 'e1', source: 'input-1', target: 'contract-1' },
      { id: 'e2', source: 'contract-1', target: 'result-1' },
    ];

    const workflow = flowToWorkflow(nodes, edges);
    const contractNode = workflow.nodes.find((n) => n.id === 'contract-1');
    expect(contractNode).toBeDefined();
    expect(contractNode?.type).toBe('contract_extract');
    expect(contractNode?.metadata?.source_variable).toBe('agent-solver');
    expect(contractNode?.metadata?.extraction_patterns).toEqual(['^\\s*([A-Za-z])\\s*$']);
  });
});

describe('workflowConverter - agent_run', () => {
  it('should convert agent_run node to backend agent_run', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: { type: 'input', label: 'Input', config: { name: 'user_prompt' } },
      },
      {
        id: 'agent-run-1',
        type: 'agent_run',
        position: { x: 220, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Novomo Agent',
          config: {
            name: 'Researcher',
            prompt: 'Investigate {{user_prompt}}',
            harness: 'claude-code',
            sandbox: 'host',
            timeoutSeconds: 120,
            retryMaxAttempts: 2,
            retryBackoffMs: 10,
          },
        },
      },
      {
        id: 'result-1',
        type: 'result',
        position: { x: 440, y: 0 },
        data: { type: 'result', label: 'Result', config: {} },
      },
    ];
    const edges: Edge[] = [
      { id: 'e1', source: 'input-1', target: 'agent-run-1' },
      { id: 'e2', source: 'agent-run-1', target: 'result-1' },
    ];

    const workflow = flowToWorkflow(nodes, edges, 'Agent Run', '', { user_prompt: 'durability' });
    const agentRun = workflow.nodes.find((n) => n.id === 'agent-run-1');
    expect(agentRun).toBeDefined();
    expect(agentRun?.type).toBe('agent_run');
    expect(agentRun?.prompt).toBe('Investigate {{user_prompt}}');
    expect(agentRun?.harness).toBe('claude-code');
    expect(agentRun?.sandbox).toBe('host');
    expect(agentRun?.timeout_seconds).toBe(120);
    expect(agentRun?.retry_policy?.max_attempts).toBe(2);
    expect(agentRun?.retry_policy?.backoff_ms).toBe(10);
  });

  it('defaults missing agent_run sandbox to docker when converting to runtime workflow', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-run-1',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Novomo Agent',
          config: {
            prompt: 'Investigate',
            harness: 'claude-code',
            timeoutSeconds: 120,
          },
        },
      },
    ];

    const workflow = flowToWorkflow(nodes, [], 'Default Agent Sandbox');
    const agentRun = workflow.nodes.find((n) => n.id === 'agent-run-1');
    expect(agentRun?.sandbox).toBe('docker');
  });

  it('preserves Codex harness when converting to runtime workflow', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-run-1',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Novomo Agent',
          config: {
            prompt: 'Investigate',
            harness: 'codex',
            timeoutSeconds: 120,
          },
        },
      },
    ];

    const workflow = flowToWorkflow(nodes, [], 'Codex Agent Run');
    const agentRun = workflow.nodes.find((n) => n.id === 'agent-run-1');
    expect(agentRun?.harness).toBe('codex');
  });

  it('converts explicit inheritance handles for agent_run nodes', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-run-1',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Novomo Agent',
          config: {
            prompt: 'Continue',
            harness: 'claude-code',
            timeoutSeconds: 120,
            inheritFromMode: 'explicit',
            inheritFromKind: 'job_run',
            inheritFromId: 'jobrun-123',
            inheritFromPolicy: 'latest',
          },
        },
      },
    ];

    const workflow = flowToWorkflow(nodes, [], 'Agent Handoff');
    const agentRun = workflow.nodes.find((n) => n.id === 'agent-run-1');
    expect(agentRun?.inherit_from).toEqual({ kind: 'job_run', id: 'jobrun-123', policy: 'latest' });
  });
});

describe('workflowConverter - novo_run', () => {
  it('should convert Superagent node to backend novo_run', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: { type: 'input', label: 'Input', config: { name: 'user_prompt' } },
      },
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 220, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            name: 'Superagent',
            prompt: 'Investigate {{user_prompt}}',
            taskSummary: 'brief',
            identity: 'sde-novo',
            sandbox: 'host',
            runtimeUrl: 'http://127.0.0.1:18082',
            timeoutSeconds: 120,
            graceSeconds: 5,
            repoSpecsJson: '[{"name":"app","source":{"type":"host_path","host_path":{"path":"/tmp/app"}}}]',
            workSourceJson: '{"type":"gitea_branch","gitea_branch":{"branch_ref":"main"}}',
            retryMaxAttempts: 2,
            retryBackoffMs: 10,
          },
        },
      },
      {
        id: 'result-1',
        type: 'result',
        position: { x: 440, y: 0 },
        data: { type: 'result', label: 'Result', config: {} },
      },
    ];
    const edges: Edge[] = [
      { id: 'e1', source: 'input-1', target: 'superagent-1' },
      { id: 'e2', source: 'superagent-1', target: 'result-1' },
    ];

    const workflow = flowToWorkflow(nodes, edges, 'Superagent Run', '', { user_prompt: 'durability' });
    const superagent = workflow.nodes.find((n) => n.id === 'superagent-1');
    expect(superagent).toBeDefined();
    expect(superagent?.type).toBe('novo_run');
    expect(superagent?.prompt).toBe('Investigate {{user_prompt}}');
    expect(superagent?.task_summary).toBe('brief');
    expect(superagent?.identity).toBe('sde-novo');
    expect(superagent?.sandbox).toBe('host');
    expect(superagent?.runtime_url).toBe('http://127.0.0.1:18082');
    expect(superagent?.timeout_seconds).toBe(120);
    expect(superagent?.grace_seconds).toBe(5);
    expect(superagent?.repo_specs?.[0]?.name).toBe('app');
    expect(superagent?.work_source?.type).toBe('gitea_branch');
    expect(superagent?.retry_policy?.max_attempts).toBe(2);
    expect(superagent?.retry_policy?.backoff_ms).toBe(10);
  });

  it('should not inject workflow inputs into task-only Superagent wakes', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: { type: 'input', label: 'Input', config: { name: 'user_prompt' } },
      },
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 220, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            taskId: 'task-existing',
            timeoutSeconds: 120,
          },
        },
      },
    ];
    const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'superagent-1' }];

    const workflow = flowToWorkflow(nodes, edges, 'Task Wake', '', { user_prompt: 'do not inject' });
    const superagent = workflow.nodes.find((n) => n.id === 'superagent-1');
    expect(superagent?.prompt).toBe('');
    expect(superagent?.task_id).toBe('task-existing');
  });

  it('should inject workflow inputs when Superagent has both task id and prompt', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'input-1',
        type: 'input',
        position: { x: 0, y: 0 },
        data: { type: 'input', label: 'Input', config: { name: 'user_prompt' } },
      },
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 220, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            taskId: 'task-existing',
            prompt: 'Use the provided input',
            timeoutSeconds: 120,
          },
        },
      },
    ];
    const edges: Edge[] = [{ id: 'e1', source: 'input-1', target: 'superagent-1' }];

    const workflow = flowToWorkflow(nodes, edges, 'Task Wake', '', { user_prompt: 'inject me' });
    const superagent = workflow.nodes.find((n) => n.id === 'superagent-1');
    expect(superagent?.prompt).toBe('Input data:\n{{user_prompt}}\n\nUse the provided input');
    expect(superagent?.task_id).toBe('task-existing');
  });

  it('should not throw when Superagent JSON config is invalid', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            prompt: 'Wake',
            timeoutSeconds: 120,
            repoSpecsJson: '{"not":"an array"}',
            workSourceJson: '[{"not":"an object"}]',
          },
        },
      },
    ];

    expect(() => flowToWorkflow(nodes, [], 'Invalid JSON')).not.toThrow();
    const workflow = flowToWorkflow(nodes, [], 'Invalid JSON');
    const superagent = workflow.nodes.find((n) => n.id === 'superagent-1');
    expect(superagent?.repo_specs).toBeUndefined();
    expect(superagent?.work_source).toBeUndefined();
  });

  it('defaults missing Superagent sandbox to docker when converting to runtime workflow', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            prompt: 'Wake',
            timeoutSeconds: 120,
          },
        },
      },
    ];

    const workflow = flowToWorkflow(nodes, [], 'Default Sandbox');
    const superagent = workflow.nodes.find((n) => n.id === 'superagent-1');
    expect(superagent?.sandbox).toBe('docker');
  });

  it('converts upstream inheritance handles for Superagent nodes', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'upstream-agent',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Upstream Agent',
          config: { prompt: 'First', harness: 'claude-code', timeoutSeconds: 120 },
        },
      },
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 220, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            prompt: 'Continue',
            timeoutSeconds: 120,
            inheritFromMode: 'upstream',
            inheritFromNodeId: 'upstream-agent',
            inheritFromPolicy: 'latest',
          },
        },
      },
    ];
    const edges: Edge[] = [{ id: 'e1', source: 'upstream-agent', target: 'superagent-1' }];

    const workflow = flowToWorkflow(nodes, edges, 'Superagent Handoff');
    const superagent = workflow.nodes.find((n) => n.id === 'superagent-1');
    expect(superagent?.inherit_from_node_id).toBe('upstream-agent');
    expect(superagent?.inherit_from_policy).toBe('latest');
  });

  it('auto-selects the nearest upstream Novomo node for Superagent handoff', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'upstream-agent',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Upstream Agent',
          config: { prompt: 'First', harness: 'claude-code', timeoutSeconds: 120 },
        },
      },
      {
        id: 'classical-agent',
        type: 'agent',
        position: { x: 220, y: 0 },
        data: {
          type: 'agent',
          label: 'Classical Agent',
          config: { model: 'openai/gpt-4o-mini', userPrompt: 'Bridge' },
        },
      },
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 440, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            prompt: 'Continue',
            timeoutSeconds: 120,
            inheritFromMode: 'auto',
            inheritFromPolicy: 'latest',
          },
        },
      },
    ];
    const edges: Edge[] = [
      { id: 'e1', source: 'upstream-agent', target: 'classical-agent' },
      { id: 'e2', source: 'classical-agent', target: 'superagent-1' },
    ];

    const workflow = flowToWorkflow(nodes, edges, 'Auto Superagent Handoff');
    const superagent = workflow.nodes.find((n) => n.id === 'superagent-1');
    expect(superagent?.inherit_from_node_id).toBe('upstream-agent');
    expect(superagent?.inherit_from_policy).toBe('latest');
  });

  it('uses workflow task handoff for automatic Novomo fan-in inheritance', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'repo',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Repo Agent',
          config: {
            prompt: 'Create repo',
            harness: 'claude-code',
            timeoutSeconds: 120,
            inheritFromMode: 'none',
          },
        },
      },
      {
        id: 'octopus',
        type: 'agent_run',
        position: { x: 220, y: -80 },
        data: {
          type: 'agent_run',
          label: 'Octopus Agent',
          config: { prompt: 'Add octopus', harness: 'claude-code', timeoutSeconds: 120, inheritFromMode: 'auto' },
        },
      },
      {
        id: 'jellyfish',
        type: 'agent_run',
        position: { x: 220, y: 80 },
        data: {
          type: 'agent_run',
          label: 'Jellyfish Agent',
          config: { prompt: 'Add jellyfish', harness: 'claude-code', timeoutSeconds: 120, inheritFromMode: 'auto' },
        },
      },
      {
        id: 'qa',
        type: 'agent_run',
        position: { x: 440, y: 0 },
        data: {
          type: 'agent_run',
          label: 'QA Agent',
          config: {
            prompt: 'Review both branches',
            harness: 'claude-code',
            timeoutSeconds: 120,
            inheritFromMode: 'auto',
            inheritFromPolicy: 'latest',
          },
        },
      },
    ];
    const edges: Edge[] = [
      { id: 'e1', source: 'repo', target: 'octopus' },
      { id: 'e2', source: 'repo', target: 'jellyfish' },
      { id: 'e3', source: 'octopus', target: 'qa' },
      { id: 'e4', source: 'jellyfish', target: 'qa' },
    ];

    const workflow = flowToWorkflow(nodes, edges, 'Agent Fan-In');
    const qa = workflow.nodes.find((n) => n.id === 'qa');
    expect(qa?.inherit_from_workflow_task).toBe(true);
    expect(qa?.inherit_from_node_id).toBeUndefined();
    expect(qa?.inherit_from).toBeUndefined();

    const validation = validateWorkflow(nodes, edges);
    expect(validation.errors.some((error) => error.nodeId === 'qa')).toBe(false);
  });
});

describe('workflowValidation - agent_run', () => {
  it('should require prompt, harness, and timeout', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-run-1',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: { type: 'agent_run', label: 'Novomo Agent', config: {} },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    expect(result.errors.some((e) => e.nodeId === 'agent-run-1' && e.message.includes('prompt'))).toBe(true);
    expect(result.errors.some((e) => e.nodeId === 'agent-run-1' && e.message.includes('harness'))).toBe(true);
    expect(result.errors.some((e) => e.nodeId === 'agent-run-1' && e.message.includes('timeout'))).toBe(true);
  });

  it('should reject invalid sandbox values', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-run-1',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Novomo Agent',
          config: {
            prompt: 'Investigate',
            harness: 'claude-code',
            sandbox: 'vm' as never,
            timeoutSeconds: 120,
          },
        },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    expect(result.errors.some((e) => e.nodeId === 'agent-run-1' && e.message.includes('invalid sandbox'))).toBe(true);
  });

  it('should reject incomplete explicit inheritance handles', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-run-1',
        type: 'agent_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Novomo Agent',
          config: {
            prompt: 'Investigate',
            harness: 'claude-code',
            timeoutSeconds: 120,
            inheritFromMode: 'explicit',
            inheritFromKind: 'job_run',
          },
        },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    expect(result.errors.some((e) => e.nodeId === 'agent-run-1' && e.message.includes('inheritance handle ID'))).toBe(
      true,
    );
  });
});

describe('workflowValidation - novo_run', () => {
  it('should require prompt or task id and timeout', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 0, y: 0 },
        data: { type: 'novo_run', label: 'Superagent', config: {} },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    expect(result.errors.some((e) => e.nodeId === 'superagent-1' && e.message.includes('prompt or task'))).toBe(true);
    expect(result.errors.some((e) => e.nodeId === 'superagent-1' && e.message.includes('timeout'))).toBe(true);
  });

  it('should reject invalid repo specs JSON', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            prompt: 'Investigate',
            timeoutSeconds: 60,
            repoSpecsJson: '{"not":"an array"}',
          },
        },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    expect(result.errors.some((e) => e.nodeId === 'superagent-1' && e.message.includes('repo specs'))).toBe(true);
  });

  it('should reject invalid work source JSON', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            prompt: 'Investigate',
            timeoutSeconds: 60,
            workSourceJson: '[{"not":"an object"}]',
          },
        },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    expect(result.errors.some((e) => e.nodeId === 'superagent-1' && e.message.includes('work source'))).toBe(true);
  });

  it('should require upstream inheritance sources to be upstream Novomo nodes', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'superagent-1',
        type: 'novo_run',
        position: { x: 0, y: 0 },
        data: {
          type: 'novo_run',
          label: 'Superagent',
          config: {
            prompt: 'Continue',
            timeoutSeconds: 60,
            inheritFromMode: 'upstream',
            inheritFromNodeId: 'downstream-agent',
          },
        },
      },
      {
        id: 'downstream-agent',
        type: 'agent_run',
        position: { x: 220, y: 0 },
        data: {
          type: 'agent_run',
          label: 'Downstream Agent',
          config: { prompt: 'Later', harness: 'claude-code', timeoutSeconds: 60 },
        },
      },
    ];
    const edges: Edge[] = [{ id: 'e1', source: 'superagent-1', target: 'downstream-agent' }];

    const result = validateWorkflow(nodes, edges);
    expect(result.isValid).toBe(false);
    expect(result.errors.some((e) => e.nodeId === 'superagent-1' && e.message.includes('upstream Novomo node'))).toBe(
      true,
    );
  });
});

describe('workflowValidation - contract_extract', () => {
  it('should error when contract_extract has no source variable', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'contract-1',
        type: 'contract_extract',
        position: { x: 0, y: 0 },
        data: {
          type: 'contract_extract',
          label: 'Contract',
          config: {
            model: 'x-ai/grok-4.1-fast',
            systemPrompt: 'Extract answer',
          },
        },
      },
    ];

    const result = validateWorkflow(nodes, []);
    expect(result.isValid).toBe(false);
    const sourceError = result.errors.find(
      (e) => e.nodeId === 'contract-1' && e.severity === 'error' && e.message.includes('source variable'),
    );
    expect(sourceError).toBeDefined();
  });
});

describe('runtimeWorkflowToWorkflowFile', () => {
  it('converts an ad-hoc runtime workflow into an editable workflow file', () => {
    const runtimeWorkflow: Workflow = {
      id: 'manual-agent-live-novomo',
      name: 'Manual Agent Live Novomo',
      nodes: [
        {
          id: 'agent1',
          type: 'agent_run',
          prompt: 'Return exactly LIVE_OK.',
          harness: 'claude-code',
          timeout_seconds: 30,
          retry_policy: {
            max_attempts: 1,
            backoff_ms: 0,
            backoff_multiply: 1,
            max_backoff_ms: 0,
          },
          metadata: {
            name: 'Live Novomo Agent',
            description: 'Ad-hoc validation run',
          },
        },
        {
          id: 'result1',
          type: 'result',
          output_name: 'final',
          aggregation_method: 'collect',
          metadata: {
            input_ids: ['agent1'],
          },
        },
      ],
      edges: [{ id: 'e1', source: 'agent1', target: 'result1' }],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow, {
      jobId: 'ff9e4fe9-3108-4bec-a87f-863e3c3ec072',
      savedWorkflowExists: false,
    });

    expect(file.id).toBe('manual-agent-live-novomo');
    expect(file.name).toBe('Manual Agent Live Novomo');
    expect(file.metadata?.source).toBe('job_snapshot');
    expect(file.metadata?.job_id).toBe('ff9e4fe9-3108-4bec-a87f-863e3c3ec072');
    expect(file.metadata?.saved_workflow_exists).toBe(false);
    expect(file.nodes).toHaveLength(2);
    expect(file.edges).toEqual([{ id: 'e1', source: 'agent1', target: 'result1' }]);

    const agentNode = file.nodes[0];
    expect(agentNode.type).toBe('agent_run');
    expect(agentNode.data.type).toBe('agent_run');
    expect(agentNode.data.label).toBe('Live Novomo Agent');
    expect(agentNode.data.config.prompt).toBe('Return exactly LIVE_OK.');
    expect(agentNode.data.config.harness).toBe('claude-code');
    expect(agentNode.data.config.sandbox).toBe('docker');
    expect(agentNode.data.config.timeoutSeconds).toBe(30);
    expect(agentNode.data.config.retryMaxAttempts).toBe(1);

    const resultNode = file.nodes[1];
    expect(resultNode.type).toBe('result');
    expect(resultNode.data.config.name).toBe('final');
    expect(resultNode.data.config.aggregationMethod).toBe('collect');
  });

  it('preserves Codex harness when converting runtime workflow into editable workflow file', () => {
    const runtimeWorkflow: Workflow = {
      id: 'manual-agent-codex',
      name: 'Manual Agent Codex',
      nodes: [
        {
          id: 'agent1',
          type: 'agent_run',
          prompt: 'Return exactly CODEX_OK.',
          harness: 'codex',
          timeout_seconds: 30,
          retry_policy: {
            max_attempts: 1,
            backoff_ms: 0,
            backoff_multiply: 1,
            max_backoff_ms: 0,
          },
        },
      ],
      edges: [],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);
    expect(file.nodes[0].data.config.harness).toBe('codex');
  });

  it('uses the platform default model when runtime prompt nodes omit a model', () => {
    const runtimeWorkflow: Workflow = {
      id: 'runtime-default-model',
      name: 'Runtime Default Model',
      nodes: [
        {
          id: 'agent-1',
          type: 'prompt',
          prompt: 'Answer directly',
          system_prompt: 'Be concise',
        },
      ],
      edges: [],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);

    expect(file.nodes[0].data.config.model).toBe(DEFAULT_MODEL);
  });

  it('preserves workflow_ref runtime fields in editor config', () => {
    const runtimeWorkflow: Workflow = {
      id: 'runtime-workflow-ref',
      name: 'Runtime Workflow Ref',
      nodes: [
        {
          id: 'ref-1',
          type: 'workflow_ref',
          workflow_ref_id: 'aggregation-synthesis',
          input_template: { user_prompt: '{{user_prompt}}' },
          output_key: 'result',
          metadata: {
            name: 'Use Aggregation',
            description: 'Compile this reference',
          },
        },
      ],
      edges: [],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);
    expect(file.nodes).toHaveLength(1);
    expect(file.nodes[0].type).toBe('workflow_ref');
    expect(file.nodes[0].data.type).toBe('workflow_ref');
    expect(file.nodes[0].data.config.workflowId).toBe('aggregation-synthesis');
    expect(file.nodes[0].data.config.workflowRefId).toBe('aggregation-synthesis');
    expect(file.nodes[0].data.config.inputTemplate).toEqual({ user_prompt: '{{user_prompt}}' });
    expect(file.nodes[0].data.config.outputKey).toBe('result');
    expect(file.nodes[0].data.config.name).toBe('Use Aggregation');
    expect(file.nodes[0].data.config.description).toBe('Compile this reference');
  });

  it('expands runtime aggregation macro metadata back into builder aggregation and result nodes', () => {
    const runtimeWorkflow: Workflow = {
      id: 'runtime-agg',
      name: 'Runtime Aggregation',
      nodes: [
        {
          id: 'agent-a',
          type: 'prompt',
          model: 'openai/gpt-4o-mini',
          prompt: 'Answer A.',
          max_tokens: 256,
        },
        {
          id: 'agent-b',
          type: 'prompt',
          model: 'anthropic/claude-sonnet-4.5',
          prompt: 'Answer B.',
          max_tokens: 256,
        },
        {
          id: 'agg-judge',
          type: 'result',
          output_name: 'final_answer',
          output_format: 'text',
          aggregation_method: 'judge',
          aggregation_config: {
            judge_model: 'openai/gpt-5-mini',
            system_prompt: 'Judge.',
            prompt: '{{responses}}',
            temperature: 0,
            max_tokens: -1,
          },
          retry_policy: {
            max_attempts: 1,
            backoff_ms: 0,
            backoff_multiply: 1,
            max_backoff_ms: 0,
          },
          metadata: {
            input_ids: ['agent-a', 'agent-b'],
            aggregation_anchor_id: 'agg-judge',
            presentation_result_id: 'final-result',
            output_name: 'final_answer',
            output_format: 'text',
          },
        },
      ],
      edges: [
        { id: 'e1', source: 'agent-a', target: 'agg-judge' },
        { id: 'e2', source: 'agent-b', target: 'agg-judge' },
      ],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);

    expect(file.nodes.map((node) => [node.id, node.type])).toEqual([
      ['agent-a', 'agent'],
      ['agent-b', 'agent'],
      ['agg-judge', 'aggregation'],
      ['final-result', 'result'],
    ]);
    expect(file.edges).toEqual([
      { id: 'e1', source: 'agent-a', target: 'agg-judge' },
      { id: 'e2', source: 'agent-b', target: 'agg-judge' },
      { id: 'agg-judge-final-result', source: 'agg-judge', target: 'final-result' },
    ]);

    const aggregationNode = file.nodes.find((node) => node.id === 'agg-judge');
    expect(aggregationNode?.data.config.aggregationMethod).toBe('judge');
    expect(aggregationNode?.data.config.aggregationConfig).toMatchObject({
      judge_model: 'openai/gpt-5-mini',
      max_tokens: -1,
    });
    expect(aggregationNode?.data.config.retryMaxAttempts).toBe(1);

    const resultNode = file.nodes.find((node) => node.id === 'final-result');
    expect(resultNode?.data.config.name).toBe('final_answer');
    expect(resultNode?.data.config.outputFormat).toBe('text');
    expect(resultNode?.data.config.aggregationMethod).toBeUndefined();
  });

  it('lays out runtime workflow snapshots as a top-to-bottom DAG', () => {
    const runtimeWorkflow: Workflow = {
      id: 'runtime-vertical-layout',
      name: 'Runtime Vertical Layout',
      nodes: [
        {
          id: 'agent-a',
          type: 'prompt',
          model: 'openai/gpt-4o-mini',
          prompt: 'Answer A.',
        },
        {
          id: 'agent-b',
          type: 'prompt',
          model: 'openai/gpt-4o-mini',
          prompt: 'Answer B.',
        },
        {
          id: 'aggregate',
          type: 'result',
          output_name: 'final',
          aggregation_method: 'judge',
          metadata: {
            aggregation_anchor_id: 'aggregate',
            presentation_result_id: 'final-result',
          },
        },
      ],
      edges: [
        { id: 'a-aggregate', source: 'agent-a', target: 'aggregate' },
        { id: 'b-aggregate', source: 'agent-b', target: 'aggregate' },
      ],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);
    const byID = new Map(file.nodes.map((node) => [node.id, node]));

    expect(byID.get('agent-a')!.position.y).toBeLessThan(byID.get('aggregate')!.position.y);
    expect(byID.get('agent-b')!.position.y).toBeLessThan(byID.get('aggregate')!.position.y);
    expect(byID.get('aggregate')!.position.y).toBeLessThan(byID.get('final-result')!.position.y);
    expect(byID.get('agent-a')!.position.x).not.toBe(byID.get('agent-b')!.position.x);
  });

  it('defaults missing runtime Superagent sandbox to docker in editor config', () => {
    const runtimeWorkflow: Workflow = {
      id: 'runtime-novo',
      name: 'Runtime Novo',
      nodes: [
        {
          id: 'superagent-1',
          type: 'novo_run',
          prompt: 'Investigate',
          timeout_seconds: 60,
        },
      ],
      edges: [],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);
    expect(file.nodes[0].data.config.sandbox).toBe('docker');
  });

  it('preserves runtime inheritance config in editor nodes', () => {
    const runtimeWorkflow: Workflow = {
      id: 'runtime-handoff',
      name: 'Runtime Handoff',
      nodes: [
        {
          id: 'agent-1',
          type: 'agent_run',
          prompt: 'Continue',
          harness: 'claude-code',
          timeout_seconds: 60,
          inherit_from: { kind: 'novo_run', id: 'nr-prior', policy: 'latest' },
        },
        {
          id: 'superagent-1',
          type: 'novo_run',
          prompt: 'Continue again',
          timeout_seconds: 60,
          inherit_from_node_id: 'agent-1',
          inherit_from_policy: 'latest',
        },
      ],
      edges: [{ id: 'e1', source: 'agent-1', target: 'superagent-1' }],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);
    expect(file.nodes[0].data.config.inheritFromMode).toBe('explicit');
    expect(file.nodes[0].data.config.inheritFromKind).toBe('novo_run');
    expect(file.nodes[0].data.config.inheritFromId).toBe('nr-prior');
    expect(file.nodes[1].data.config.inheritFromMode).toBe('upstream');
    expect(file.nodes[1].data.config.inheritFromNodeId).toBe('agent-1');
  });

  it('preserves runtime operation nodes in editor nodes', () => {
    const runtimeWorkflow: Workflow = {
      id: 'runtime-operation',
      name: 'Runtime Operation',
      nodes: [
        {
          id: 'vote-counter',
          type: 'operation',
          operation_type: 'count_votes',
          operation_config: { answers: ['A', 'B', 'A'] },
          metadata: { name: 'Count votes', description: 'Tally final answers' },
        },
      ],
      edges: [],
    };

    const file = runtimeWorkflowToWorkflowFile(runtimeWorkflow);

    expect(file.nodes[0].type).toBe('operation');
    expect(file.nodes[0].data.type).toBe('operation');
    expect(file.nodes[0].data.config.operationType).toBe('count_votes');
    expect(file.nodes[0].data.config.operationConfig).toEqual({ answers: ['A', 'B', 'A'] });
    expect(file.nodes[0].data.config.name).toBe('Count votes');
    expect(file.nodes[0].data.config.description).toBe('Tally final answers');
  });

  it('validates operation node configuration', () => {
    const baseNode: Node<NodeData> = {
      id: 'op-1',
      type: 'operation',
      position: { x: 0, y: 0 },
      data: {
        type: 'operation',
        label: 'Operation',
        config: {},
      },
    };

    const missingType = validateWorkflow([baseNode], []);
    expect(missingType.isValid).toBe(false);
    expect(missingType.errors.some((error) => error.message.includes('has no operation type'))).toBe(true);

    const missingConfig = validateWorkflow(
      [
        {
          ...baseNode,
          data: {
            ...baseNode.data,
            config: { operationType: 'count_votes' },
          },
        },
      ],
      [],
    );
    expect(missingConfig.isValid).toBe(false);
    expect(missingConfig.errors.some((error) => error.message.includes('requires one of: answers'))).toBe(true);

    const valid = validateWorkflow(
      [
        {
          ...baseNode,
          data: {
            ...baseNode.data,
            config: { operationType: 'count_votes', operationConfig: { answers: '{{answers}}' } },
          },
        },
      ],
      [],
    );
    expect(valid.isValid).toBe(true);

    const emptyInputIDs = validateWorkflow(
      [
        {
          ...baseNode,
          data: {
            ...baseNode.data,
            config: { operationType: 'collect_inputs', operationConfig: { input_ids: [] } },
          },
        },
      ],
      [],
    );
    expect(emptyInputIDs.isValid).toBe(false);

    const selectWinnerWithAnswers = validateWorkflow(
      [
        {
          ...baseNode,
          data: {
            ...baseNode.data,
            config: {
              operationType: 'select_winner',
              operationConfig: { winner_answer: 'A', answers: ['A', 'B'] },
            },
          },
        },
      ],
      [],
    );
    expect(selectWinnerWithAnswers.isValid).toBe(true);
  });
});
