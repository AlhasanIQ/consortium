import { describe, expect, it } from 'bun:test';
import type { AdminJobDetailResponse, AgentRunSummary, NodeMetric } from '../../api/adminClient';
import {
  adminActionErrorMessage,
  agentRunKindLabel,
  agentRunNovomoIDLinks,
  canStopAgentRun,
  enrichNodesWithChildJobIDs,
} from './adminJobDetailUtils';

type ChildJob = AdminJobDetailResponse['ChildJobs'][number];

function childNode(id: string, childJobID?: string): NodeMetric {
  return {
    id: 1,
    node_id: id,
    node_order: 1,
    node_type: 'child_workflow',
    status: 'completed',
    tokens_input: 0,
    tokens_output: 0,
    cost: 0,
    latency_ms: 0,
    WidthPercent: 0,
    StartOffsetPercent: 0,
    TimelineDurationMs: 0,
    ActiveDurationMs: 0,
    WaitDurationMs: 0,
    DisplayOrder: '1',
    IsChildNode: false,
    IsParentNode: false,
    ChildJobID: childJobID,
  };
}

function promptNode(id: string): NodeMetric {
  return {
    id: 2,
    node_id: id,
    node_order: 2,
    node_type: 'prompt',
    status: 'completed',
    tokens_input: 0,
    tokens_output: 0,
    cost: 0,
    latency_ms: 0,
    WidthPercent: 0,
    StartOffsetPercent: 0,
    TimelineDurationMs: 0,
    ActiveDurationMs: 0,
    WaitDurationMs: 0,
    DisplayOrder: '2',
    IsChildNode: false,
    IsParentNode: false,
  };
}

function childJob(id: string): ChildJob {
  return { id, description: `Child ${id}`, status: 'running', model: 'workflow', tokens_total: 0, cost: 0 };
}

function agentRun(overrides: Partial<AgentRunSummary> = {}): AgentRunSummary {
  return {
    id: 'job:run:agent:1',
    job_id: 'job',
    execution_id: 'job',
    run_id: 'run',
    node_id: 'agent',
    attempt: 1,
    run_kind: 'agent_run',
    external_run_id: 'novomo-run',
    harness: 'claude-code',
    status: 'running',
    tokens_input: 0,
    tokens_output: 0,
    cost_usd: 0,
    created_at: '2026-05-30T00:00:00Z',
    updated_at: '2026-05-30T00:00:00Z',
    ...overrides,
  };
}

describe('enrichNodesWithChildJobIDs', () => {
  it('assigns child job ID to unresolved child_workflow node', () => {
    const nodes = [childNode('child-node-1'), promptNode('prompt-1')];
    const jobs = [childJob('child-job-abc')];

    const enriched = enrichNodesWithChildJobIDs(nodes, jobs);

    expect(enriched[0].ChildJobID).toBe('child-job-abc');
    expect(enriched[1].ChildJobID).toBeUndefined();
  });

  it('keeps existing ChildJobID when already set', () => {
    const nodes = [childNode('child-node-1', 'existing-child-job')];
    const jobs = [childJob('new-child-job')];

    const enriched = enrichNodesWithChildJobIDs(nodes, jobs);

    expect(enriched[0].ChildJobID).toBe('existing-child-job');
  });

  it('returns nodes unchanged when no child jobs exist', () => {
    const nodes = [childNode('child-node-1'), promptNode('prompt-1')];

    const enriched = enrichNodesWithChildJobIDs(nodes, []);

    expect(enriched).toBe(nodes);
  });

  it('assigns multiple child jobs to multiple child_workflow nodes in order', () => {
    const nodes = [childNode('child-a'), promptNode('prompt-1'), childNode('child-b')];
    const jobs = [childJob('job-1'), childJob('job-2')];

    const enriched = enrichNodesWithChildJobIDs(nodes, jobs);

    expect(enriched[0].ChildJobID).toBe('job-1');
    expect(enriched[1].ChildJobID).toBeUndefined();
    expect(enriched[2].ChildJobID).toBe('job-2');
  });
});

describe('canStopAgentRun', () => {
  it('allows active Novomo-backed runs with an external ID', () => {
    expect(canStopAgentRun(agentRun({ status: 'pending' }))).toBe(true);
    expect(canStopAgentRun(agentRun({ status: 'running', run_kind: 'novo_run' }))).toBe(true);
  });

  it('rejects terminal or unpersisted runs', () => {
    expect(canStopAgentRun(agentRun({ status: 'completed' }))).toBe(false);
    expect(canStopAgentRun(agentRun({ status: 'failed' }))).toBe(false);
    expect(canStopAgentRun(agentRun({ external_run_id: '' }))).toBe(false);
  });
});

describe('agentRunKindLabel', () => {
  it('uses Superagent for novo_run rows', () => {
    expect(agentRunKindLabel('novo_run')).toBe('Superagent');
    expect(agentRunKindLabel('agent_run')).toBe('Agent run');
  });
});

describe('agentRunNovomoIDLinks', () => {
  it('labels Agent run Novomo job and job-run IDs', () => {
    const links = agentRunNovomoIDLinks(
      agentRun({
        run_kind: 'agent_run',
        external_run_id: 'job-123',
        external_job_run_id: 'jobrun-123',
        novomo_urls: [
          { kind: 'job', label: 'Novomo Job', id: 'job-123', url: 'http://127.0.0.1:5174/jobs/job-123' },
          {
            kind: 'job_run',
            label: 'Novomo JobRun',
            id: 'jobrun-123',
            url: 'http://127.0.0.1:5174/job-runs/jobrun-123',
          },
        ],
      }),
    );

    expect(links).toEqual([
      { label: 'Job', id: 'job-123', url: 'http://127.0.0.1:5174/jobs/job-123' },
      { label: 'JobRun', id: 'jobrun-123', url: 'http://127.0.0.1:5174/job-runs/jobrun-123' },
    ]);
  });

  it('labels Superagent Novomo run and task IDs', () => {
    const links = agentRunNovomoIDLinks(
      agentRun({
        run_kind: 'novo_run',
        external_run_id: 'nr-123',
        external_task_id: 'task-123',
        novomo_urls: [
          { kind: 'novo_run', label: 'NovoRun', id: 'nr-123', url: 'http://127.0.0.1:5174/novo-runs/nr-123' },
          { kind: 'task', label: 'Task', id: 'task-123', url: 'http://127.0.0.1:5174/tasks/task-123' },
        ],
      }),
    );

    expect(links).toEqual([
      { label: 'NovoRun', id: 'nr-123', url: 'http://127.0.0.1:5174/novo-runs/nr-123' },
      { label: 'Task', id: 'task-123', url: 'http://127.0.0.1:5174/tasks/task-123' },
    ]);
  });
});

describe('adminActionErrorMessage', () => {
  it('maps Novomo not_stoppable conflicts to operator text', () => {
    const err = { response: { status: 409, data: { error: 'not_stoppable' } } };

    expect(adminActionErrorMessage(err)).toBe('This external run is already finished or cannot be stopped.');
  });
});
