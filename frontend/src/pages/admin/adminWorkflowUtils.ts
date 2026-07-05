import type { AdminWorkflowStats } from '@/api/adminClient';

export interface WorkflowReferenceGroup {
  label: string;
  ids: string[];
}

export const BENCHMARK_L0_WARNING = 'Benchmark references L0 directly';

export function workflowReferenceGroups(workflow: AdminWorkflowStats): WorkflowReferenceGroup[] {
  const groups: WorkflowReferenceGroup[] = [];
  addReferenceGroup(groups, 'aggregation', workflow.AggregationSourceIDs);
  addReferenceGroup(groups, 'workflow_ref', workflow.WorkflowRefIDs);
  addReferenceGroup(groups, 'child_workflow', workflow.ChildWorkflowIDs);
  return groups;
}

export function benchmarkL0WarningLabel(workflow: AdminWorkflowStats): string {
  return workflow.ReferencesL0DirectlyAsBenchmark ? BENCHMARK_L0_WARNING : '';
}

function addReferenceGroup(groups: WorkflowReferenceGroup[], label: string, ids: string[] | undefined) {
  const cleanIDs = sortedUnique(ids ?? []);
  if (cleanIDs.length > 0) {
    groups.push({ label, ids: cleanIDs });
  }
}

function sortedUnique(ids: string[]): string[] {
  return [...new Set(ids.map((id) => id.trim()).filter(Boolean))].sort();
}
