import { describe, expect, it } from 'bun:test';
import type { AdminWorkflowStats } from '@/api/adminClient';
import { BENCHMARK_L0_WARNING, benchmarkL0WarningLabel, workflowReferenceGroups } from './adminWorkflowUtils';

function workflow(overrides: Partial<AdminWorkflowStats> = {}): AdminWorkflowStats {
  return {
    id: 'reasoning-demo',
    name: 'Reasoning Demo',
    description: '',
    Layer: 'L1',
    ExecutionCount: 0,
    SuccessCount: 0,
    SuccessRate: 0,
    TotalCost: 0,
    TotalTokens: 0,
    NodeCount: 0,
    NodeTypes: {},
    ModelsUsed: [],
    AggregationSourceIDs: [],
    WorkflowRefIDs: [],
    ChildWorkflowIDs: [],
    ReferencesL0DirectlyAsBenchmark: false,
    ...overrides,
  };
}

describe('workflowReferenceGroups', () => {
  it('groups compact source relationships in stable order', () => {
    expect(
      workflowReferenceGroups(
        workflow({
          AggregationSourceIDs: [' aggregation-judge ', ''],
          WorkflowRefIDs: ['reasoning-b', ' reasoning-a ', 'reasoning-a', '  '],
          ChildWorkflowIDs: ['reasoning-child', 'reasoning-child'],
        }),
      ),
    ).toEqual([
      { label: 'aggregation', ids: ['aggregation-judge'] },
      { label: 'workflow_ref', ids: ['reasoning-a', 'reasoning-b'] },
      { label: 'child_workflow', ids: ['reasoning-child'] },
    ]);
  });

  it('omits empty relationship groups', () => {
    expect(workflowReferenceGroups(workflow())).toEqual([]);
  });
});

describe('benchmarkL0WarningLabel', () => {
  it('returns warning text only for workflows flagged by the API', () => {
    expect(benchmarkL0WarningLabel(workflow({ ReferencesL0DirectlyAsBenchmark: true }))).toBe(BENCHMARK_L0_WARNING);
    expect(benchmarkL0WarningLabel(workflow({ ReferencesL0DirectlyAsBenchmark: false }))).toBe('');
  });
});
