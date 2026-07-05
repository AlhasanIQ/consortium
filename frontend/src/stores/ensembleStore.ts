import { create } from 'zustand';
import type { ConnectionState } from '../types/connection';
import type { AggregationDetails, AggregationMethod, SeedWorkflowInfo } from '../types/workflow';
import { DEFAULT_WORKFLOW_ID, extractAgentsFromWorkflow, loadWorkflowById } from '../utils/ensembleSync';

export type { ConnectionState } from '../types/connection';
// Re-export types for convenience
export type { AggregationDetails } from '../types/workflow';

export type AgentStatus = 'idle' | 'receiving' | 'thinking' | 'streaming' | 'done' | 'error';

// Execution state machine - prevents duplicate submissions
export type ExecutionState =
  | { status: 'idle' }
  | { status: 'submitting'; idempotencyKey: string } // POST request in flight
  | { status: 'streaming'; jobId: string; idempotencyKey: string } // WebSocket connected
  | { status: 'complete'; jobId: string }
  | { status: 'cancelled'; jobId: string } // Cancelled by user request
  | { status: 'error'; message: string; jobId?: string };

// Export the workflow ID for use in other components
export { DEFAULT_WORKFLOW_ID };

// Generate a unique idempotency key for each user submission
export function generateIdempotencyKey(prompt: string): string {
  const timestamp = Date.now();
  const random = Math.random().toString(36).substring(2, 10);
  // Include a hash of the prompt to make it content-aware
  const promptHash = prompt
    .split('')
    .reduce((a, b) => {
      a = (a << 5) - a + b.charCodeAt(0);
      return a & a;
    }, 0)
    .toString(36);
  return `ensemble-${timestamp}-${promptHash}-${random}`;
}

export interface AgentState {
  id: string;
  model: string;
  provider: string;
  displayName: string;
  color: string;
  status: AgentStatus;
  output: string;
  streamingText: string;
  tokens: number;
  cost: number;
  latencyMs: number;
  error?: string;
}

export interface FlowParticle {
  id: string;
  fromCenter: boolean; // true = center->agent, false = agent->center
  agentId: string;
  progress: number; // 0-1
  startTime: number;
}

export interface HistoryEntry {
  id: string;
  jobId?: string; // Backend job ID for linking to admin panel
  prompt: string;
  synthesizedResponse: string;
  timestamp: Date;
  totalCost: number;
  totalTokens: number;
  totalLatency: number;
  status?: 'completed' | 'failed' | 'running' | 'pending' | 'cancelled';
  aggregationMethod?: AggregationMethod;
  aggregationDetails?: AggregationDetails;
}

export interface EnsembleState {
  // Prompt & output
  prompt: string;
  synthesizedOutput: string;
  synthesisStreaming: string;

  // Execution state machine (prevents duplicate submissions)
  executionState: ExecutionState;

  // Execution status
  jobId: string | null;
  isExecuting: boolean;
  isAggregating: boolean;
  aggregationMethod: AggregationMethod | null;
  aggregationDetails: AggregationDetails | null;
  phase: 'idle' | 'sending' | 'thinking' | 'aggregating' | 'complete';

  // Workflow selection
  selectedWorkflowId: string;
  availableWorkflows: SeedWorkflowInfo[];

  // Agents
  agents: AgentState[];
  selectedAgentId: string | null;

  // Animations
  particles: FlowParticle[];

  // Totals
  totalCost: number;
  totalTokens: number;
  totalLatency: number;

  // History
  history: HistoryEntry[];
  backendHistory: HistoryEntry[]; // History fetched from backend
  historyLoading: boolean;

  // Connection state (for WebSocket resilience)
  connectionState: ConnectionState;
  lastSequence: number; // For event deduplication

  // Draft persistence
  hasSavedDraft: boolean;

  // Actions
  setPrompt: (prompt: string) => void;
  addAgent: (model: string, provider: string) => void;
  removeAgent: (id: string) => void;
  setSelectedAgent: (id: string | null) => void;

  // Workflow selection actions
  setSelectedWorkflow: (id: string) => void;
  setAvailableWorkflows: (workflows: SeedWorkflowInfo[]) => void;

  // Execution actions
  startExecution: (jobId: string) => void;
  updateAgentStatus: (agentId: string, status: AgentStatus, data?: Partial<AgentState>) => void;
  appendAgentStream: (agentId: string, chunk: string, tokens?: number) => void;
  completeAgent: (agentId: string, output: string, latencyMs: number, cost: number, tokens?: number) => void;
  failAgent: (agentId: string, error: string) => void;

  // Aggregation actions (generalized from synthesis)
  startAggregation: (method?: AggregationMethod) => void;
  appendAggregationStream: (chunk: string) => void;
  completeAggregation: (output: string, totalCost: number, totalTokens?: number, details?: AggregationDetails) => void;
  setAggregationDetails: (details: AggregationDetails | null) => void;

  // Particles
  addParticle: (agentId: string, fromCenter: boolean) => void;
  updateParticles: () => void;
  removeParticle: (id: string) => void;

  // Reset
  reset: () => void;
  clearHistory: () => void;

  // Backend history
  fetchBackendHistory: () => Promise<void>;

  // State machine transitions
  startSubmitting: (idempotencyKey: string) => boolean; // Returns false if not allowed
  setStreaming: (jobId: string) => void;
  setExecutionError: (message: string, jobId?: string) => void;
  setExecutionComplete: (
    jobId: string,
    completionData?: { finalOutput?: string; totalCost?: number; totalTokens?: number },
  ) => void;
  setExecutionCancelled: (jobId: string) => void;
  canExecute: () => boolean; // Check if execution is allowed

  // Connection state actions
  setConnectionState: (state: ConnectionState) => void;
  updateLastSequence: (sequence: number) => void;

  // Draft persistence actions
  saveDraft: () => void;
  loadDraft: () => boolean; // Returns true if draft was loaded
  clearDraft: () => void;
}

// Provider color mapping
const providerColors: Record<string, string> = {
  anthropic: '#D97706', // Orange/amber for Claude
  openai: '#10B981', // Green for GPT
  google: '#3B82F6', // Blue for Gemini
  meta: '#8B5CF6', // Purple for Llama
  mistral: '#EC4899', // Pink for Mistral
  deepseek: '#06B6D4', // Cyan for DeepSeek
  xai: '#EF4444', // Red for Grok/xAI
  'x-ai': '#EF4444', // Red for Grok/xAI (alternate)
  qwen: '#F59E0B', // Amber for Qwen
  default: '#6B7280', // Gray default
};

const getProviderColor = (provider: string, model?: string): string => {
  const lowerProvider = provider.toLowerCase();

  // For OpenRouter, extract provider from model ID (e.g., "anthropic/claude-..." -> "anthropic")
  if (lowerProvider === 'openrouter' && model) {
    const modelProvider = model.split('/')[0].toLowerCase();
    for (const [key, color] of Object.entries(providerColors)) {
      if (modelProvider.includes(key)) return color;
    }
  }

  for (const [key, color] of Object.entries(providerColors)) {
    if (lowerProvider.includes(key)) return color;
  }
  return providerColors.default;
};

const getDisplayName = (model: string): string => {
  // Extract friendly name from model ID
  const modelLower = model.toLowerCase();

  // Common model name mappings
  if (modelLower.includes('claude')) return 'Claude';
  if (modelLower.includes('gpt-4')) return 'GPT-4';
  if (modelLower.includes('gpt-3')) return 'GPT-3.5';
  if (modelLower.includes('gemini')) return 'Gemini';
  if (modelLower.includes('llama')) return 'Llama';
  if (modelLower.includes('mistral')) return 'Mistral';
  if (modelLower.includes('deepseek')) return 'DeepSeek';
  if (modelLower.includes('grok')) return 'Grok';
  if (modelLower.includes('qwen')) return 'Qwen';
  if (modelLower.includes('o1')) return 'o1';
  if (modelLower.includes('o3')) return 'o3';

  // Fallback: extract last part after /
  const parts = model.split('/');
  const modelName = parts[parts.length - 1];
  // Capitalize first letter and limit length
  return modelName.charAt(0).toUpperCase() + modelName.slice(1, 11);
};

// Default agents - must match workflow IDs for proper node_id matching
// These are fallbacks if workflow fails to load from API
const defaultAgents: AgentState[] = [
  {
    id: 'agent-claude', // Must match workflow node ID
    model: 'anthropic/claude-sonnet-4.5',
    provider: 'openrouter',
    displayName: 'Claude',
    color: providerColors.anthropic,
    status: 'idle',
    output: '',
    streamingText: '',
    tokens: 0,
    cost: 0,
    latencyMs: 0,
  },
  {
    id: 'agent-gemini', // Must match workflow node ID
    model: 'google/gemini-2.5-flash',
    provider: 'openrouter',
    displayName: 'Gemini',
    color: providerColors.google,
    status: 'idle',
    output: '',
    streamingText: '',
    tokens: 0,
    cost: 0,
    latencyMs: 0,
  },
  {
    id: 'agent-gpt5mini', // Must match workflow node ID
    model: 'openai/gpt-5-mini',
    provider: 'openrouter',
    displayName: 'GPT-5 Mini',
    color: providerColors.openai,
    status: 'idle',
    output: '',
    streamingText: '',
    tokens: 0,
    cost: 0,
    latencyMs: 0,
  },
];

export const useEnsembleStore = create<EnsembleState>((set, get) => ({
  // Initial state
  prompt: '',
  synthesizedOutput: '',
  synthesisStreaming: '',
  executionState: { status: 'idle' },
  jobId: null,
  isExecuting: false,
  isAggregating: false,
  aggregationMethod: null,
  aggregationDetails: null,
  phase: 'idle',
  selectedWorkflowId: 'reasoning-informed-captain-synthesis',
  availableWorkflows: [],
  agents: defaultAgents,
  selectedAgentId: null,
  particles: [],
  totalCost: 0,
  totalTokens: 0,
  totalLatency: 0,
  history: [],
  backendHistory: [],
  historyLoading: false,
  connectionState: { status: 'disconnected', attempt: 0, maxAttempts: 5, lastSequence: 0 },
  lastSequence: 0,
  hasSavedDraft: false,

  // Basic setters
  setPrompt: (prompt) => set({ prompt }),

  addAgent: (model, provider) => {
    const agents = get().agents;
    const newAgent: AgentState = {
      id: `agent-${Date.now()}`,
      model,
      provider,
      displayName: getDisplayName(model),
      color: getProviderColor(provider, model),
      status: 'idle',
      output: '',
      streamingText: '',
      tokens: 0,
      cost: 0,
      latencyMs: 0,
    };
    set({ agents: [...agents, newAgent] });
  },

  removeAgent: (id) => {
    const agents = get().agents.filter((a) => a.id !== id);
    set({ agents });
  },

  setSelectedAgent: (id) => set({ selectedAgentId: id }),

  // Workflow selection actions
  setSelectedWorkflow: (id) => set({ selectedWorkflowId: id }),
  setAvailableWorkflows: (workflows) => set({ availableWorkflows: workflows }),

  // Execution actions
  startExecution: (jobId) => {
    const agents = get().agents.map((a) => ({
      ...a,
      status: 'receiving' as AgentStatus,
      output: '',
      streamingText: '',
      tokens: 0,
      cost: 0,
      latencyMs: 0,
      error: undefined,
    }));

    set({
      jobId,
      isExecuting: true,
      phase: 'sending',
      agents,
      synthesizedOutput: '',
      synthesisStreaming: '',
      totalCost: 0,
      totalTokens: 0,
      totalLatency: 0,
    });

    // Add outgoing particles for each agent
    agents.forEach((agent) => {
      get().addParticle(agent.id, true);
    });
  },

  updateAgentStatus: (agentId, status, data) => {
    const agents = get().agents.map((a) => (a.id === agentId ? { ...a, status, ...data } : a));
    set({ agents });

    // Update phase based on agent statuses
    const allThinking = agents.every((a) => a.status === 'thinking' || a.status === 'streaming');
    if (allThinking && get().phase === 'sending') {
      set({ phase: 'thinking' });
    }
  },

  appendAgentStream: (agentId, chunk, tokens) => {
    const agents = get().agents.map((a) =>
      a.id === agentId
        ? {
            ...a,
            streamingText: a.streamingText + chunk,
            tokens: tokens ?? a.tokens,
            status: 'streaming' as AgentStatus,
          }
        : a,
    );
    set({ agents });
  },

  completeAgent: (agentId, output, latencyMs, cost, tokens) => {
    const agents = get().agents.map((a) =>
      a.id === agentId
        ? {
            ...a,
            status: 'done' as AgentStatus,
            output,
            latencyMs,
            cost,
            tokens: tokens ?? a.tokens,
            streamingText: output,
          }
        : a,
    );

    // Add return particle
    get().addParticle(agentId, false);

    // Calculate totals
    const totalCost = agents.reduce((sum, a) => sum + a.cost, 0);
    const totalTokens = agents.reduce((sum, a) => sum + a.tokens, 0);
    const totalLatency = Math.max(...agents.filter((a) => a.status === 'done').map((a) => a.latencyMs));

    set({ agents, totalCost, totalTokens, totalLatency });
  },

  failAgent: (agentId, error) => {
    const agents = get().agents.map((a) => (a.id === agentId ? { ...a, status: 'error' as AgentStatus, error } : a));
    set({ agents });
  },

  // Aggregation actions (generalized from synthesis)
  startAggregation: (method = 'synthesis') => {
    set({
      isAggregating: true,
      aggregationMethod: method,
      aggregationDetails: null, // Clear previous details
      phase: 'aggregating',
      synthesisStreaming: '',
    });
  },

  appendAggregationStream: (chunk) => {
    set((state) => ({
      synthesisStreaming: state.synthesisStreaming + chunk,
    }));
  },

  completeAggregation: (output, totalCost, totalTokens, details) => {
    const state = get();
    const finalTokens = totalTokens ?? state.totalTokens;
    const historyEntry: HistoryEntry = {
      id: `history-${Date.now()}`,
      jobId: state.jobId ?? undefined,
      prompt: state.prompt,
      synthesizedResponse: output,
      timestamp: new Date(),
      totalCost,
      totalTokens: finalTokens,
      totalLatency: state.totalLatency,
      aggregationMethod: state.aggregationMethod ?? undefined,
      aggregationDetails: details,
    };

    set({
      synthesizedOutput: '', // Clear so only history shows the result (prevents duplicate)
      synthesisStreaming: '',
      isAggregating: false,
      aggregationMethod: null,
      aggregationDetails: details ?? null,
      isExecuting: false,
      phase: 'complete',
      totalCost,
      totalTokens: finalTokens,
      history: [historyEntry, ...state.history].slice(0, 20), // Keep last 20
    });
  },

  setAggregationDetails: (details) => set({ aggregationDetails: details }),

  // Particle management
  addParticle: (agentId, fromCenter) => {
    const particle: FlowParticle = {
      id: `particle-${Date.now()}-${Math.random()}`,
      agentId,
      fromCenter,
      progress: 0,
      startTime: Date.now(),
    };
    set((state) => ({ particles: [...state.particles, particle] }));
  },

  updateParticles: () => {
    const now = Date.now();
    const duration = 800; // ms for particle to travel

    set((state) => ({
      particles: state.particles
        .map((p) => ({
          ...p,
          progress: Math.min(1, (now - p.startTime) / duration),
        }))
        .filter((p) => p.progress < 1),
    }));
  },

  removeParticle: (id) => {
    set((state) => ({
      particles: state.particles.filter((p) => p.id !== id),
    }));
  },

  // Reset
  reset: () => {
    set({
      prompt: '',
      synthesizedOutput: '',
      synthesisStreaming: '',
      jobId: null,
      isExecuting: false,
      isAggregating: false,
      aggregationMethod: null,
      aggregationDetails: null,
      phase: 'idle',
      agents: get().agents.map((a) => ({
        ...a,
        status: 'idle',
        output: '',
        streamingText: '',
        tokens: 0,
        cost: 0,
        latencyMs: 0,
        error: undefined,
      })),
      selectedAgentId: null,
      particles: [],
      totalCost: 0,
      totalTokens: 0,
      totalLatency: 0,
    });
  },

  clearHistory: () => set({ history: [], backendHistory: [] }),

  // Fetch history from backend API
  fetchBackendHistory: async () => {
    set({ historyLoading: true });
    try {
      const response = await fetch('/api/jobs?limit=20');
      if (!response.ok) {
        throw new Error(`Failed to fetch history: ${response.statusText}`);
      }
      const data = await response.json();
      const backendHistory: HistoryEntry[] = (data.jobs || []).map(
        (job: {
          id: string;
          description: string;
          status: string;
          cost: number;
          tokens_total: number;
          created_at: string;
          aggregation_method?: string;
          result_text?: string;
          prompt?: string;
        }) => ({
          id: `backend-${job.id}`,
          jobId: job.id,
          prompt: job.prompt || job.description || '',
          synthesizedResponse: job.result_text || '',
          timestamp: new Date(job.created_at),
          totalCost: job.cost || 0,
          totalTokens: job.tokens_total || 0,
          totalLatency: 0, // Not available in list view
          status: job.status as HistoryEntry['status'],
          aggregationMethod: job.aggregation_method as AggregationMethod | undefined,
        }),
      );
      set({ backendHistory, historyLoading: false });
    } catch (error) {
      console.error('[EnsembleStore] Failed to fetch backend history:', error);
      set({ historyLoading: false });
    }
  },

  // State machine transitions - ensures only valid state transitions occur
  canExecute: () => {
    const state = get().executionState;
    return (
      state.status === 'idle' || state.status === 'complete' || state.status === 'error' || state.status === 'cancelled'
    );
  },

  startSubmitting: (idempotencyKey: string) => {
    const state = get().executionState;
    // Only allow transition from idle, complete, error, or cancelled
    if (
      state.status !== 'idle' &&
      state.status !== 'complete' &&
      state.status !== 'error' &&
      state.status !== 'cancelled'
    ) {
      console.warn('[EnsembleStore] Cannot start submission: already in progress', state.status);
      return false;
    }
    set({
      executionState: { status: 'submitting', idempotencyKey },
      isExecuting: true,
      phase: 'sending',
    });
    console.log('[EnsembleStore] State transition: -> submitting (key:', idempotencyKey.slice(-8), ')');
    return true;
  },

  setStreaming: (jobId: string) => {
    const state = get().executionState;
    if (state.status !== 'submitting') {
      console.warn('[EnsembleStore] Invalid transition to streaming from:', state.status);
      return;
    }
    set({
      executionState: { status: 'streaming', jobId, idempotencyKey: state.idempotencyKey },
      jobId,
    });
    console.log('[EnsembleStore] State transition: submitting -> streaming (job:', jobId.slice(-8), ')');
  },

  setExecutionError: (message: string, jobId?: string) => {
    set({
      executionState: { status: 'error', message, jobId },
      isExecuting: false,
      phase: 'idle',
    });
    console.log('[EnsembleStore] State transition: -> error:', message);
  },

  setExecutionComplete: (
    jobId: string,
    completionData?: { finalOutput?: string; totalCost?: number; totalTokens?: number },
  ) => {
    const state = get();
    const updates: Partial<EnsembleState> = {
      executionState: { status: 'complete', jobId },
    };

    // Patch the latest history entry with authoritative data from the
    // workflow-level "complete" event when fields are missing (covers
    // "collect" aggregation where the result node emits empty output).
    if (completionData && state.history.length > 0) {
      const latest = state.history[0];
      const needsPatch =
        (completionData.finalOutput && !latest.synthesizedResponse) ||
        (completionData.totalCost && !latest.totalCost) ||
        (completionData.totalTokens && !latest.totalTokens);

      if (needsPatch) {
        const patched = [...state.history];
        patched[0] = {
          ...latest,
          synthesizedResponse: latest.synthesizedResponse || completionData.finalOutput || '',
          totalCost: latest.totalCost || completionData.totalCost || 0,
          totalTokens: latest.totalTokens || completionData.totalTokens || 0,
        };
        updates.history = patched;
      }
    }

    set(updates);
    console.log('[EnsembleStore] State transition: -> complete (job:', jobId.slice(-8), ')');
  },

  setExecutionCancelled: (jobId: string) => {
    set({
      executionState: { status: 'cancelled', jobId },
      isExecuting: false,
      phase: 'idle',
    });
    console.log('[EnsembleStore] State transition: -> cancelled (job:', jobId.slice(-8), ')');
  },

  // Connection state actions
  setConnectionState: (connectionState: ConnectionState) => {
    set({ connectionState });
    console.log('[EnsembleStore] Connection state:', connectionState.status);
  },

  updateLastSequence: (sequence: number) => {
    set({ lastSequence: sequence });
  },

  // Draft persistence actions
  saveDraft: () => {
    const state = get();
    const draft = {
      prompt: state.prompt,
      selectedWorkflowId: state.selectedWorkflowId,
      timestamp: Date.now(),
    };
    try {
      localStorage.setItem('ensemble-draft', JSON.stringify(draft));
      set({ hasSavedDraft: true });
      console.log('[EnsembleStore] Draft saved');
    } catch (err) {
      console.warn('[EnsembleStore] Failed to save draft:', err);
    }
  },

  loadDraft: (): boolean => {
    try {
      const stored = localStorage.getItem('ensemble-draft');
      if (!stored) return false;

      const draft = JSON.parse(stored);
      // Expire drafts after 24 hours
      const maxAge = 24 * 60 * 60 * 1000;
      if (Date.now() - draft.timestamp > maxAge) {
        localStorage.removeItem('ensemble-draft');
        return false;
      }

      set({
        prompt: draft.prompt || '',
        selectedWorkflowId: draft.selectedWorkflowId || 'reasoning-informed-captain-synthesis',
        hasSavedDraft: true,
      });
      console.log('[EnsembleStore] Draft loaded');
      return true;
    } catch (err) {
      console.warn('[EnsembleStore] Failed to load draft:', err);
      return false;
    }
  },

  clearDraft: () => {
    try {
      localStorage.removeItem('ensemble-draft');
      set({ hasSavedDraft: false });
      console.log('[EnsembleStore] Draft cleared');
    } catch (err) {
      console.warn('[EnsembleStore] Failed to clear draft:', err);
    }
  },
}));

let ensembleInitPromise: Promise<void> | null = null;

export const initEnsembleAgents = (): Promise<void> => {
  if (ensembleInitPromise) {
    return ensembleInitPromise;
  }

  ensembleInitPromise = (async () => {
    try {
      const workflow = await loadWorkflowById(DEFAULT_WORKFLOW_ID);
      if (workflow) {
        const agents = extractAgentsFromWorkflow(workflow);
        if (agents.length > 0) {
          // Only update agents if not currently executing (avoid overwriting in-progress state)
          const currentState = useEnsembleStore.getState();
          if (!currentState.isExecuting && currentState.executionState.status === 'idle') {
            useEnsembleStore.setState({ agents });
            console.log('[EnsembleStore] Loaded', agents.length, 'agents from workflow');
          } else {
            console.log('[EnsembleStore] Skipping agent init - execution in progress');
          }
        }
      }
    } catch (err) {
      console.warn('[EnsembleStore] Failed to load agents from workflow, using defaults:', err);
    }
  })();

  return ensembleInitPromise;
};
