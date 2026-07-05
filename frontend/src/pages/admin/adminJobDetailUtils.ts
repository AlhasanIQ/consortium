import type { AdminJobDetailResponse, AgentRunSummary, NodeMetric } from '../../api/adminClient';

type ChildJob = AdminJobDetailResponse['ChildJobs'][number];

// enrichNodesWithChildJobIDs assigns ChildJobID to child_workflow nodes that
// don't already have one, using the ChildJobs array from the API response.
// This works in all job states (running, failed, completed) because ChildJobs
// is derived from the DB relationship (parent_execution_id), not from metadata.
export function enrichNodesWithChildJobIDs(nodes: NodeMetric[], childJobs: ChildJob[]): NodeMetric[] {
  if (nodes.length === 0 || childJobs.length === 0) {
    return nodes;
  }

  // Collect child_workflow nodes that need a ChildJobID.
  const unresolved = nodes.filter((n) => n.node_type === 'child_workflow' && !n.ChildJobID);
  if (unresolved.length === 0) {
    return nodes;
  }

  // Single child job, single unresolved node — direct assignment.
  if (unresolved.length === 1 && childJobs.length === 1) {
    const targetID = unresolved[0].node_id;
    return nodes.map((node) => (node.node_id === targetID ? { ...node, ChildJobID: childJobs[0].id } : node));
  }

  // Multiple children: assign round-robin by order (nodes and child jobs are
  // both sorted by creation time from the backend).
  const assignable = [...childJobs];
  return nodes.map((node) => {
    if (node.node_type !== 'child_workflow' || node.ChildJobID || assignable.length === 0) {
      return node;
    }
    return { ...node, ChildJobID: assignable.shift()!.id };
  });
}

export function canStopAgentRun(run: AgentRunSummary): boolean {
  const status = run.status.trim().toLowerCase();
  const kind = (run.run_kind || 'agent_run').trim().toLowerCase();
  return (
    (kind === 'agent_run' || kind === 'novo_run') &&
    run.external_run_id.trim().length > 0 &&
    (status === 'pending' || status === 'running' || status === 'started')
  );
}

export function agentRunKindLabel(kind: string): string {
  return kind.trim().toLowerCase() === 'novo_run' ? 'Superagent' : 'Agent run';
}

export interface AgentRunNovomoIDLink {
  label: string;
  id: string;
  url?: string;
}

export function agentRunNovomoIDLinks(run: AgentRunSummary): AgentRunNovomoIDLink[] {
  const kind = (run.run_kind || 'agent_run').trim().toLowerCase();
  const urlByKind = new Map((run.novomo_urls || []).map((link) => [link.kind.trim().toLowerCase(), link.url]));
  const links: AgentRunNovomoIDLink[] = [];
  const add = (label: string, id: string | undefined, urlKind: string) => {
    const trimmed = id?.trim();
    if (!trimmed) return;
    links.push({ label, id: trimmed, url: urlByKind.get(urlKind) });
  };

  if (kind === 'novo_run') {
    add('NovoRun', run.external_run_id, 'novo_run');
    add('Task', run.external_task_id, 'task');
    add('JobRun', run.external_job_run_id, 'job_run');
    return links;
  }

  add('Job', run.external_run_id, 'job');
  add('JobRun', run.external_job_run_id, 'job_run');
  add('Task', run.external_task_id, 'task');
  return links;
}

export function adminActionErrorMessage(err: unknown): string {
  const response = (err as { response?: { status?: number; data?: { error?: string } } })?.response;
  const apiMessage = response?.data?.error?.trim();
  if (response?.status === 404) {
    return apiMessage || 'This external run is no longer available.';
  }
  if (response?.status === 409) {
    if (apiMessage && !apiMessage.toLowerCase().includes('not_stoppable')) {
      return apiMessage;
    }
    return 'This external run is already finished or cannot be stopped.';
  }
  if (!response) {
    return err instanceof Error ? err.message : 'Could not reach the Consortium admin API.';
  }
  return apiMessage || 'Failed to stop the external run.';
}
