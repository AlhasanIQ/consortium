import type { AggregationMethod } from '../types/workflow';

export const AGGREGATION_WORKFLOW_ID_BY_METHOD: Record<AggregationMethod, string> = {
  collect: 'aggregation-collect',
  majority_vote: 'aggregation-majority-vote',
  synthesis: 'aggregation-synthesis',
  judge: 'aggregation-judge',
  debate_decide: 'aggregation-debate-decide',
  scoring: 'aggregation-scoring',
  peer_matrix: 'aggregation-peer-matrix',
};

const AGGREGATION_METHOD_BY_WORKFLOW_ID = Object.fromEntries(
  Object.entries(AGGREGATION_WORKFLOW_ID_BY_METHOD).map(([method, workflowId]) => [workflowId, method]),
) as Record<string, AggregationMethod>;

export function aggregationWorkflowIdForMethod(method: AggregationMethod | undefined): string | undefined {
  return method ? AGGREGATION_WORKFLOW_ID_BY_METHOD[method] : undefined;
}

export function aggregationMethodForWorkflowId(workflowId: string | undefined): AggregationMethod | undefined {
  const id = workflowId?.trim();
  return id ? AGGREGATION_METHOD_BY_WORKFLOW_ID[id] : undefined;
}
