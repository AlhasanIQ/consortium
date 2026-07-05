import type { Edge, Node } from '@xyflow/react';
import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import { type Model, modelsClient } from '../../api/modelsClient';
import { workflowClient } from '../../api/workflowClient';
import { useWorkflowStore } from '../../stores/workflowStore';
import {
  type AggregationMethod,
  DEFAULT_NOVOMO_SANDBOX,
  ensureAggregationConfigDefaults,
  type NodeConfig,
  type NodeData,
} from '../../types/workflow';
import { aggregationMethodForWorkflowId, aggregationWorkflowIdForMethod } from '../../utils/aggregationWorkflowMapping';
import { findNearestNovomoHandoffSources } from '../../utils/novomoHandoff';
import { flowToWorkflowFile } from '../../utils/workflowConverter';
import ModelSelector from '../common/ModelSelector';
import AggregationConfigSection from './AggregationConfigs';
import {
  HELP_TEXT_STYLE,
  INPUT_STYLE,
  LABEL_STYLE,
  RETRY_INPUT_STYLE,
  RETRY_LABEL_STYLE,
  SELECT_STYLE,
  TEXTAREA_STYLE,
} from './configPanelStyles';

const nextTemplateKey = (current: Record<string, string>): string => {
  let index = Object.keys(current).length + 1;
  while (current[`key_${index}`] !== undefined) {
    index += 1;
  }
  return `key_${index}`;
};

let templateRowCounter = 0;

interface TemplateRow {
  id: string;
  key: string;
  value: string;
}

const createTemplateRows = (value?: Record<string, string>): TemplateRow[] =>
  Object.entries(value || {}).map(([key, rowValue]) => ({
    id: `template-row-${templateRowCounter++}`,
    key,
    value: rowValue,
  }));

const rowsToTemplateConfig = (rows: TemplateRow[]): Record<string, string> => {
  const next: Record<string, string> = {};
  const seen = new Set<string>();
  for (const row of rows) {
    if (!row.key || !row.value || seen.has(row.key)) {
      continue;
    }
    seen.add(row.key);
    next[row.key] = row.value;
  }
  return next;
};

const templateSignature = (value?: Record<string, string>): string => JSON.stringify(value || {});

const OPERATION_TYPES = [
  'collect_inputs',
  'format_candidates',
  'extract_answer',
  'count_votes',
  'group_answer_camps',
  'select_winner',
  'reduce_scores',
  'parse_json_field',
  'parse_selection',
] as const;

const aggregationActionErrorMessage = (error: unknown, fallback: string): string => {
  const responseData = (error as { response?: { data?: { message?: unknown; details?: unknown } } })?.response?.data;
  const message = typeof responseData?.message === 'string' ? responseData.message : undefined;
  const details = responseData?.details;
  const detailText =
    typeof details === 'string'
      ? details
      : details && typeof details === 'object'
        ? JSON.stringify(details)
        : undefined;

  if (message && detailText) {
    return `${message}: ${detailText}`;
  }
  if (message) {
    return message;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return fallback;
};

/** Reusable retry policy config section */
const RetryPolicySection: React.FC<{
  config: NodeConfig;
  onChange: (key: string, value: unknown) => void;
  disabled?: boolean;
}> = ({ config, onChange, disabled = false }) => (
  <details style={{ marginBottom: '15px' }}>
    <summary style={{ cursor: 'pointer', fontSize: '14px', fontWeight: '500', marginBottom: '8px' }}>
      Retry Policy
    </summary>
    <div style={{ paddingLeft: '8px', borderLeft: '2px solid #e0e0e0' }}>
      <div style={{ marginBottom: '10px' }}>
        <label htmlFor="config-retry-max" style={RETRY_LABEL_STYLE}>
          Max Attempts (1-10)
        </label>
        <input
          id="config-retry-max"
          type="number"
          min={1}
          max={10}
          value={config.retryMaxAttempts ?? 3}
          onChange={(e) => onChange('retryMaxAttempts', parseInt(e.target.value, 10))}
          disabled={disabled}
          style={RETRY_INPUT_STYLE}
        />
      </div>
      <div style={{ marginBottom: '10px' }}>
        <label htmlFor="config-retry-backoff" style={RETRY_LABEL_STYLE}>
          Initial Backoff (ms)
        </label>
        <input
          id="config-retry-backoff"
          type="number"
          min={100}
          value={config.retryBackoffMs ?? 1000}
          onChange={(e) => onChange('retryBackoffMs', parseInt(e.target.value, 10))}
          disabled={disabled}
          style={RETRY_INPUT_STYLE}
        />
      </div>
      <div style={{ marginBottom: '10px' }}>
        <label htmlFor="config-retry-multiplier" style={RETRY_LABEL_STYLE}>
          Backoff Multiplier
        </label>
        <input
          id="config-retry-multiplier"
          type="number"
          min={1}
          step={0.1}
          value={config.retryBackoffMultiplier ?? 2.0}
          onChange={(e) => onChange('retryBackoffMultiplier', parseFloat(e.target.value))}
          disabled={disabled}
          style={RETRY_INPUT_STYLE}
        />
      </div>
      <div style={{ marginBottom: '10px' }}>
        <label htmlFor="config-retry-max-backoff" style={RETRY_LABEL_STYLE}>
          Max Backoff (ms)
        </label>
        <input
          id="config-retry-max-backoff"
          type="number"
          min={100}
          value={config.retryMaxBackoffMs ?? 30000}
          onChange={(e) => onChange('retryMaxBackoffMs', parseInt(e.target.value, 10))}
          disabled={disabled}
          style={RETRY_INPUT_STYLE}
        />
      </div>
    </div>
  </details>
);

const SandboxSelect: React.FC<{
  id: string;
  config: NodeConfig;
  onChange: (key: string, value: unknown) => void;
}> = ({ id, config, onChange }) => (
  <>
    <label htmlFor={id} style={LABEL_STYLE}>
      Sandbox
    </label>
    <select
      id={id}
      value={config.sandbox || DEFAULT_NOVOMO_SANDBOX}
      onChange={(e) => onChange('sandbox', e.target.value)}
      style={SELECT_STYLE}
    >
      <option value="docker">Docker</option>
      <option value="host">Host</option>
    </select>
  </>
);

const NovomoHandoffSection: React.FC<{
  config: NodeConfig;
  onChange: (key: string, value: unknown) => void;
  nodes: Node<NodeData>[];
  edges: Edge[];
  selectedNodeId: string;
}> = ({ config, onChange, nodes, edges, selectedNodeId }) => {
  const mode = config.inheritFromMode || 'auto';
  const upstreamOptions = nodes.filter(
    (node) => node.id !== selectedNodeId && (node.data.type === 'agent_run' || node.data.type === 'novo_run'),
  );
  const autoSources = findNearestNovomoHandoffSources(selectedNodeId, nodes, edges);
  const autoSource = autoSources.length === 1 ? autoSources[0] : undefined;
  const autoSourceLabel = autoSource || (autoSources.length > 1 ? `Workflow task (${autoSources.join(', ')})` : 'None');

  return (
    <details style={{ marginBottom: '15px' }} open={mode !== 'none'}>
      <summary style={{ cursor: 'pointer', fontSize: '14px', fontWeight: '500', marginBottom: '8px' }}>
        Inherit From
      </summary>
      <div style={{ paddingLeft: '8px', borderLeft: '2px solid #e0e0e0' }}>
        <div style={{ marginBottom: '10px' }}>
          <label htmlFor="config-inherit-mode" style={RETRY_LABEL_STYLE}>
            Source
          </label>
          <select
            id="config-inherit-mode"
            value={mode}
            onChange={(e) => onChange('inheritFromMode', e.target.value)}
            style={SELECT_STYLE}
          >
            <option value="auto">Auto</option>
            <option value="none">None</option>
            <option value="upstream">Upstream node</option>
            <option value="explicit">Explicit handle</option>
          </select>
        </div>

        {mode === 'auto' && (
          <>
            <div style={{ marginBottom: '10px' }}>
              <label htmlFor="config-inherit-auto-node" style={RETRY_LABEL_STYLE}>
                Node
              </label>
              <input
                id="config-inherit-auto-node"
                type="text"
                value={autoSourceLabel}
                readOnly
                style={RETRY_INPUT_STYLE}
              />
            </div>
            <div style={{ marginBottom: '10px' }}>
              <label htmlFor="config-inherit-policy-auto" style={RETRY_LABEL_STYLE}>
                Policy
              </label>
              <input
                id="config-inherit-policy-auto"
                type="text"
                value={config.inheritFromPolicy || ''}
                onChange={(e) => onChange('inheritFromPolicy', e.target.value)}
                style={RETRY_INPUT_STYLE}
                placeholder="Novomo default"
              />
            </div>
          </>
        )}

        {mode === 'upstream' && (
          <>
            <div style={{ marginBottom: '10px' }}>
              <label htmlFor="config-inherit-node" style={RETRY_LABEL_STYLE}>
                Node
              </label>
              <select
                id="config-inherit-node"
                value={config.inheritFromNodeId || ''}
                onChange={(e) => onChange('inheritFromNodeId', e.target.value)}
                style={SELECT_STYLE}
              >
                <option value="">Select node</option>
                {upstreamOptions.map((node) => (
                  <option key={node.id} value={node.id}>
                    {node.data.label || node.id}
                  </option>
                ))}
              </select>
            </div>
            <div style={{ marginBottom: '10px' }}>
              <label htmlFor="config-inherit-policy-upstream" style={RETRY_LABEL_STYLE}>
                Policy
              </label>
              <input
                id="config-inherit-policy-upstream"
                type="text"
                value={config.inheritFromPolicy || ''}
                onChange={(e) => onChange('inheritFromPolicy', e.target.value)}
                style={RETRY_INPUT_STYLE}
                placeholder="Novomo default"
              />
            </div>
          </>
        )}

        {mode === 'explicit' && (
          <>
            <div style={{ display: 'grid', gridTemplateColumns: '120px 1fr', gap: '8px', marginBottom: '10px' }}>
              <div>
                <label htmlFor="config-inherit-kind" style={RETRY_LABEL_STYLE}>
                  Type
                </label>
                <select
                  id="config-inherit-kind"
                  value={config.inheritFromKind || 'job_run'}
                  onChange={(e) => onChange('inheritFromKind', e.target.value)}
                  style={SELECT_STYLE}
                >
                  <option value="job_run">Job Run</option>
                  <option value="job">Job</option>
                  <option value="novo_run">Novo Run</option>
                  <option value="task">Task</option>
                </select>
              </div>
              <div>
                <label htmlFor="config-inherit-id" style={RETRY_LABEL_STYLE}>
                  ID
                </label>
                <input
                  id="config-inherit-id"
                  type="text"
                  value={config.inheritFromId || ''}
                  onChange={(e) => onChange('inheritFromId', e.target.value)}
                  style={RETRY_INPUT_STYLE}
                  placeholder="Novomo ID"
                />
              </div>
            </div>
            <div style={{ marginBottom: '10px' }}>
              <label htmlFor="config-inherit-policy-explicit" style={RETRY_LABEL_STYLE}>
                Policy
              </label>
              <input
                id="config-inherit-policy-explicit"
                type="text"
                value={config.inheritFromPolicy || ''}
                onChange={(e) => onChange('inheritFromPolicy', e.target.value)}
                style={RETRY_INPUT_STYLE}
                placeholder="Novomo default"
              />
            </div>
          </>
        )}
      </div>
    </details>
  );
};

const InputTemplateEditor: React.FC<{
  value?: Record<string, string>;
  description: React.ReactNode;
  resetKey: string;
  disabled?: boolean;
  onChange: (value: Record<string, string>) => void;
}> = ({ value, description, resetKey, disabled = false, onChange }) => {
  const [rows, setRows] = useState<TemplateRow[]>(() => createTemplateRows(value));
  const valueSig = templateSignature(value);
  const lastCommittedSignatureRef = useRef(valueSig);
  const lastResetKeyRef = useRef(resetKey);

  useEffect(() => {
    if (resetKey === lastResetKeyRef.current) {
      return;
    }
    setRows(createTemplateRows(value));
    lastCommittedSignatureRef.current = valueSig;
    lastResetKeyRef.current = resetKey;
  }, [resetKey, value, valueSig]);

  useEffect(() => {
    if (valueSig === lastCommittedSignatureRef.current) {
      return;
    }
    setRows(createTemplateRows(value));
    lastCommittedSignatureRef.current = valueSig;
  }, [valueSig, value]);

  const commitRows = (nextRows: TemplateRow[]) => {
    setRows(nextRows);
    const nextConfig = rowsToTemplateConfig(nextRows);
    lastCommittedSignatureRef.current = templateSignature(nextConfig);
    onChange(nextConfig);
  };

  const visibleKeys = rows.reduce<Record<string, string>>((keys, row) => {
    if (row.key) {
      keys[row.key] = row.value;
    }
    return keys;
  }, {});
  const keyCounts = rows.reduce<Record<string, number>>((counts, row) => {
    if (row.key) {
      counts[row.key] = (counts[row.key] || 0) + 1;
    }
    return counts;
  }, {});

  return (
    <div style={{ marginBottom: '15px' }}>
      <span style={LABEL_STYLE}>Input Template</span>
      <div style={HELP_TEXT_STYLE}>{description}</div>
      {rows.map((row) => (
        <div key={row.id} style={{ display: 'flex', gap: '4px', marginBottom: '4px' }}>
          <input
            type="text"
            value={row.key}
            onChange={(e) => {
              const nextRows = rows.map((candidate) =>
                candidate.id === row.id ? { ...candidate, key: e.target.value } : candidate,
              );
              commitRows(nextRows);
            }}
            style={{
              ...INPUT_STYLE,
              flex: 1,
              fontSize: '12px',
              border: !row.key || keyCounts[row.key] > 1 ? '1px solid #f44336' : INPUT_STYLE.border,
            }}
            disabled={disabled}
            placeholder="key"
          />
          <input
            type="text"
            value={row.value}
            onChange={(e) => {
              const nextRows = rows.map((candidate) =>
                candidate.id === row.id ? { ...candidate, value: e.target.value } : candidate,
              );
              commitRows(nextRows);
            }}
            style={{
              ...INPUT_STYLE,
              flex: 1,
              fontSize: '12px',
              fontFamily: 'monospace',
              border: !row.value ? '1px solid #f44336' : INPUT_STYLE.border,
            }}
            disabled={disabled}
            placeholder="{{var}}"
          />
          <button
            type="button"
            disabled={disabled}
            onClick={() => {
              const nextRows = rows.filter((candidate) => candidate.id !== row.id);
              commitRows(nextRows);
            }}
            style={{
              background: '#ef4444',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              padding: '4px 8px',
              cursor: 'pointer',
              fontSize: '12px',
              opacity: disabled ? 0.65 : 1,
            }}
          >
            x
          </button>
        </div>
      ))}
      <button
        type="button"
        disabled={disabled}
        onClick={() => {
          const newKey = nextTemplateKey(visibleKeys);
          commitRows([...rows, { id: `template-row-${templateRowCounter++}`, key: newKey, value: '' }]);
        }}
        style={{
          padding: '4px 12px',
          fontSize: '12px',
          backgroundColor: '#e0e0e0',
          border: 'none',
          borderRadius: '4px',
          cursor: 'pointer',
          marginTop: '4px',
          opacity: disabled ? 0.65 : 1,
        }}
      >
        + Add mapping
      </button>
    </div>
  );
};

const ConfigPanel: React.FC = () => {
  const {
    nodes,
    edges,
    selectedNodeId,
    updateNodeConfig,
    deleteNode,
    updateNodeAlias,
    isAliasValid,
    expandAggregationNode,
    collapseAggregationNode,
    forkAggregationNode,
  } = useWorkflowStore();
  const selectedNode = nodes.find((n) => n.id === selectedNodeId);
  const selectedNodeType = selectedNode?.data.type;
  const selectedResultHasIncomingAggregation =
    selectedNodeType === 'result' &&
    edges.some((edge) => {
      if (edge.target !== selectedNodeId) return false;
      const source = nodes.find((node) => node.id === edge.source);
      return source?.data.type === 'aggregation';
    });
  const isAggregationConfigNode =
    selectedNodeType === 'aggregation' || (selectedNodeType === 'result' && !selectedResultHasIncomingAggregation);

  const [localConfig, setLocalConfig] = useState<NodeConfig>({});
  const [models, setModels] = useState<Model[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [schemaText, setSchemaText] = useState<string>('');
  const [aliasInput, setAliasInput] = useState<string>('');
  const [aliasError, setAliasError] = useState<string | null>(null);
  const [availableWorkflows, setAvailableWorkflows] = useState<{ id: string; name: string }[]>([]);
  const [operationConfigText, setOperationConfigText] = useState<string>('{}');
  const [operationConfigError, setOperationConfigError] = useState<string | null>(null);
  const [aggregationActionError, setAggregationActionError] = useState<string | null>(null);
  const [aggregationActionLoading, setAggregationActionLoading] = useState(false);
  const selectedOperationConfigText =
    selectedNode?.data.type === 'operation'
      ? JSON.stringify(selectedNode.data.config.operationConfig || {}, null, 2)
      : '{}';

  useEffect(() => {
    if (selectedNode) {
      setLocalConfig(selectedNode.data.config);
      // Initialize schema text from config
      const schema = selectedNode.data.config.schema || selectedNode.data.config.outputSchema;
      setSchemaText(schema ? JSON.stringify(schema, null, 2) : '');
      // Initialize alias input from config or node ID
      setAliasInput(selectedNode.data.config.alias || selectedNode.id);
      setAliasError(null);
      setOperationConfigError(null);
      setAggregationActionError(null);
    }
  }, [selectedNode]);

  useEffect(() => {
    setOperationConfigText(selectedOperationConfigText);
  }, [selectedOperationConfigText]);

  // Fetch models when agent node selected or a node with LLM aggregation config is selected.
  useEffect(() => {
    const needsModels =
      selectedNodeType === 'agent' ||
      selectedNodeType === 'contract_extract' ||
      (isAggregationConfigNode &&
        ['synthesis', 'judge', 'scoring', 'peer_matrix', 'debate_decide'].includes(
          localConfig.aggregationMethod || '',
        ));

    if (needsModels) {
      setLoadingModels(true);
      modelsClient
        .getModels('openrouter')
        .then((fetchedModels) => {
          setModels(fetchedModels);
          setLoadingModels(false);
        })
        .catch((error) => {
          console.error('Failed to fetch models:', error);
          setLoadingModels(false);
        });
    }
  }, [selectedNodeType, isAggregationConfigNode, localConfig.aggregationMethod]);

  // Fetch available workflows when workflow composition or aggregation source node selected.
  useEffect(() => {
    if (
      selectedNodeType !== 'child_workflow' &&
      selectedNodeType !== 'workflow_ref' &&
      selectedNodeType !== 'aggregation'
    ) {
      return;
    }
    workflowClient
      .listWorkflows()
      .then((data) => {
        const workflows = (data || []).map((w: { id: string; name?: string }) => ({
          id: w.id,
          name: w.name || w.id,
        }));
        setAvailableWorkflows(workflows);
      })
      .catch((err) => console.error('Failed to fetch workflows:', err));
  }, [selectedNodeType]);

  if (!selectedNode) {
    return null;
  }

  const expandedAggregationVisible = Boolean(
    selectedNodeId &&
      nodes.some(
        (node) =>
          node.data.config.aggregationInternalState === 'expanded' &&
          node.data.config.aggregationAnchorId === selectedNodeId,
      ),
  );
  const isReadOnlySourceNode =
    localConfig.aggregationInternalState === 'expanded' ||
    localConfig.sourceLocked === true ||
    expandedAggregationVisible;
  const aggregationWorkflowId = localConfig.aggregationWorkflowId || '';
  const aggregationWorkflowOptions = availableWorkflows.filter(
    (workflow) => workflow.id.startsWith('aggregation-') || workflow.id === aggregationWorkflowId,
  );

  const handleConfigChange = (key: string, value: unknown) => {
    if (isReadOnlySourceNode) return;
    const newConfig = { ...localConfig, [key]: value };
    setLocalConfig(newConfig);
    updateNodeConfig(selectedNodeId!, newConfig);
  };

  const handleAggregationWorkflowChange = (workflowId: string) => {
    if (isReadOnlySourceNode) return;
    const sourceMethod = aggregationMethodForWorkflowId(workflowId);
    const newConfig = {
      ...localConfig,
      aggregationWorkflowId: workflowId,
      ...(sourceMethod && sourceMethod !== localConfig.aggregationMethod
        ? {
            aggregationMethod: sourceMethod,
            aggregationConfig: ensureAggregationConfigDefaults(sourceMethod, undefined),
          }
        : {}),
    };
    setLocalConfig(newConfig);
    updateNodeConfig(selectedNodeId!, newConfig);
  };

  const handleAggregationMethodChange = (method: AggregationMethod) => {
    if (isReadOnlySourceNode) return;
    const newConfig = {
      ...localConfig,
      aggregationMethod: method,
      aggregationWorkflowId: aggregationWorkflowIdForMethod(method) || localConfig.aggregationWorkflowId,
      aggregationConfig: ensureAggregationConfigDefaults(method, undefined),
    };
    setLocalConfig(newConfig);
    updateNodeConfig(selectedNodeId!, newConfig);
  };

  const handleWorkflowRefIdChange = (workflowID: string) => {
    if (isReadOnlySourceNode) return;
    const newConfig = { ...localConfig, workflowRefId: workflowID, workflowId: workflowID };
    setLocalConfig(newConfig);
    updateNodeConfig(selectedNodeId!, newConfig);
  };

  const handleExpandAggregation = async () => {
    if (!selectedNodeId) return;
    setAggregationActionError(null);
    setAggregationActionLoading(true);
    try {
      // flowToWorkflowFile strips any existing expanded preview before asking
      // the backend for the compiled view, so Refresh compiles the parent graph.
      const workflowFile = flowToWorkflowFile(nodes, edges, 'Aggregation Preview');
      const preview = await workflowClient.compilePreview({ workflow_file: workflowFile });
      expandAggregationNode(selectedNodeId, preview);
    } catch (error) {
      setAggregationActionError(aggregationActionErrorMessage(error, 'Failed to expand aggregation source'));
    } finally {
      setAggregationActionLoading(false);
    }
  };

  const handleForkAggregation = async () => {
    if (!selectedNodeId) return;
    setAggregationActionError(null);
    setAggregationActionLoading(true);
    try {
      const workflowFile = flowToWorkflowFile(nodes, edges, 'Aggregation Fork');
      const preview = await workflowClient.compilePreview({ workflow_file: workflowFile });
      forkAggregationNode(selectedNodeId, preview);
    } catch (error) {
      setAggregationActionError(aggregationActionErrorMessage(error, 'Failed to fork aggregation source'));
    } finally {
      setAggregationActionLoading(false);
    }
  };

  const handleModelChange = (modelId: string) => {
    if (isReadOnlySourceNode) return;
    // Auto-set provider to openrouter when a model is selected
    const newConfig = { ...localConfig, provider: 'openrouter', model: modelId };
    setLocalConfig(newConfig);
    updateNodeConfig(selectedNodeId!, newConfig);
  };

  const handleDelete = () => {
    if (isReadOnlySourceNode) return;
    if (confirm('Are you sure you want to delete this node?')) {
      deleteNode(selectedNodeId!);
    }
  };

  const handleAliasBlur = () => {
    if (isReadOnlySourceNode) return;
    if (!selectedNodeId || !aliasInput.trim()) {
      setAliasError('Reference ID cannot be empty');
      return;
    }

    // Skip if alias hasn't changed
    const currentAlias = selectedNode?.data.config.alias || selectedNode?.id;
    if (aliasInput === currentAlias) {
      setAliasError(null);
      return;
    }

    // Validate and update
    const validation = isAliasValid(aliasInput, selectedNodeId);
    if (!validation.valid) {
      setAliasError(validation.error || 'Invalid Reference ID');
      return;
    }

    const result = updateNodeAlias(selectedNodeId, aliasInput);
    if (!result.success) {
      setAliasError(result.error || 'Failed to update Reference ID');
    } else {
      setAliasError(null);
    }
  };

  return (
    <div style={{ overflowY: 'auto', flex: 1 }}>
      <h3 style={{ margin: '0 0 15px 0', fontSize: '16px' }}>
        Configure{' '}
        {selectedNode.data.type
          .split('_')
          .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
          .join(' ')}
      </h3>

      {isReadOnlySourceNode && (
        <div
          style={{
            marginBottom: '15px',
            padding: '10px',
            backgroundColor: '#f1f5f9',
            border: '1px solid #cbd5e1',
            borderRadius: '6px',
            color: '#334155',
            fontSize: '12px',
            lineHeight: 1.4,
          }}
        >
          Expanded aggregation previews are read-only. Collapse the preview or fork the aggregation before editing.
        </div>
      )}

      <div style={{ marginBottom: '15px' }}>
        <label htmlFor="config-alias" style={LABEL_STYLE}>
          {selectedNode.data.type === 'input' ? 'Node ID' : 'Reference ID'}
        </label>
        <div style={HELP_TEXT_STYLE}>
          {selectedNode.data.type === 'input' ? (
            'Identifies this node in the workflow. Use schema keys to reference input values.'
          ) : (
            <>
              Use{' '}
              <code
                style={{
                  background: '#e8e8e8',
                  padding: '2px 5px',
                  borderRadius: '3px',
                  fontFamily: 'monospace',
                  fontSize: '11px',
                }}
              >
                {`{{${aliasInput || selectedNode.id}}}`}
              </code>{' '}
              to reference this node's output
            </>
          )}
        </div>
        <input
          id="config-alias"
          type="text"
          value={aliasInput}
          onChange={(e) => {
            setAliasInput(e.target.value);
            setAliasError(null);
          }}
          onBlur={handleAliasBlur}
          disabled={isReadOnlySourceNode}
          style={{
            ...INPUT_STYLE,
            border: aliasError ? '1px solid #f44336' : '1px solid #ddd',
            fontFamily: 'monospace',
            opacity: isReadOnlySourceNode ? 0.75 : 1,
          }}
          placeholder={selectedNode.data.type === 'input' ? 'e.g., user-input' : 'e.g., agent-claude'}
        />
        {aliasError && <div style={{ color: '#f44336', fontSize: '12px', marginTop: '4px' }}>{aliasError}</div>}
      </div>

      <div style={{ marginBottom: '15px' }}>
        <label htmlFor="config-name" style={LABEL_STYLE}>
          Name
        </label>
        <input
          id="config-name"
          type="text"
          value={localConfig.name || ''}
          onChange={(e) => handleConfigChange('name', e.target.value)}
          disabled={isReadOnlySourceNode}
          style={INPUT_STYLE}
          placeholder="Node name"
        />
      </div>

      <div style={{ marginBottom: '15px' }}>
        <label htmlFor="config-description" style={LABEL_STYLE}>
          Description
        </label>
        <textarea
          id="config-description"
          value={localConfig.description || ''}
          onChange={(e) => handleConfigChange('description', e.target.value)}
          disabled={isReadOnlySourceNode}
          style={{
            ...TEXTAREA_STYLE,
            fontSize: '14px',
            fontFamily: 'inherit',
            minHeight: '60px',
          }}
          placeholder="Node description"
        />
      </div>

      {(selectedNode.data.type === 'agent' || selectedNode.data.type === 'contract_extract') && (
        <>
          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-model" style={LABEL_STYLE}>
              Model
            </label>
            <ModelSelector
              models={models}
              value={localConfig.model || ''}
              onChange={handleModelChange}
              disabled={loadingModels || isReadOnlySourceNode}
              loading={loadingModels}
              placeholder="Search models..."
            />
          </div>

          <div
            style={{
              marginBottom: '15px',
              padding: '12px',
              backgroundColor: '#e3f2fd',
              borderRadius: '6px',
              border: '1px solid #90caf9',
            }}
          >
            <label
              htmlFor="config-system-prompt"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '6px',
                marginBottom: '5px',
                fontSize: '14px',
                fontWeight: '600',
                color: '#1565c0',
              }}
            >
              <span style={{ fontSize: '16px' }}>🤖</span> System Prompt
            </label>
            <div style={HELP_TEXT_STYLE}>
              Defines the agent's role, persona, and behavior. This sets context for how the agent should respond. Use{' '}
              <code style={{ background: '#e8e8e8', padding: '1px 4px', borderRadius: '3px' }}>{'{{variable}}'}</code>{' '}
              for input interpolation.
            </div>
            <textarea
              id="config-system-prompt"
              value={localConfig.systemPrompt || ''}
              onChange={(e) => handleConfigChange('systemPrompt', e.target.value)}
              disabled={isReadOnlySourceNode}
              style={{
                ...TEXTAREA_STYLE,
                border: '1px solid #90caf9',
                minHeight: '100px',
                backgroundColor: 'white',
              }}
              placeholder="You are a helpful assistant specialized in..."
            />
          </div>

          <div
            style={{
              marginBottom: '15px',
              padding: '12px',
              backgroundColor: '#fff3e0',
              borderRadius: '6px',
              border: '1px solid #ffcc80',
            }}
          >
            <label
              htmlFor="config-user-prompt"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '6px',
                marginBottom: '5px',
                fontSize: '14px',
                fontWeight: '600',
                color: '#e65100',
              }}
            >
              <span style={{ fontSize: '16px' }}>💬</span> User Prompt
              <span style={{ fontSize: '11px', fontWeight: 'normal', color: '#888' }}>(optional)</span>
            </label>
            <div style={HELP_TEXT_STYLE}>
              The actual task or question sent to the agent. If empty, input variables are automatically used.
            </div>
            <textarea
              id="config-user-prompt"
              value={localConfig.userPrompt || ''}
              onChange={(e) => handleConfigChange('userPrompt', e.target.value)}
              disabled={isReadOnlySourceNode}
              style={{
                ...TEXTAREA_STYLE,
                border: '1px solid #ffcc80',
                minHeight: '60px',
                backgroundColor: 'white',
              }}
              placeholder="Leave empty to auto-use input variables (e.g., {{topic}})"
            />
          </div>

          {selectedNode.data.type === 'contract_extract' && (
            <>
              <div style={{ marginBottom: '15px' }}>
                <label htmlFor="config-source-variable" style={LABEL_STYLE}>
                  Source Variable
                </label>
                <div style={HELP_TEXT_STYLE}>
                  Upstream context key that contains the text to extract from (for example: <code>agent-solver</code>).
                </div>
                <input
                  id="config-source-variable"
                  type="text"
                  value={localConfig.sourceVariable || ''}
                  onChange={(e) => handleConfigChange('sourceVariable', e.target.value)}
                  disabled={isReadOnlySourceNode}
                  style={{ ...INPUT_STYLE, fontFamily: 'monospace' }}
                  placeholder="e.g., child-reasoning-judge-pick"
                />
              </div>

              <div style={{ marginBottom: '15px' }}>
                <label htmlFor="config-extraction-patterns" style={LABEL_STYLE}>
                  Extraction Patterns (one regex per line)
                </label>
                <textarea
                  id="config-extraction-patterns"
                  value={(localConfig.extractionPatterns || []).join('\n')}
                  onChange={(e) => {
                    const patterns = e.target.value
                      .split('\n')
                      .map((line) => line.trim())
                      .filter((line) => line.length > 0);
                    handleConfigChange('extractionPatterns', patterns);
                  }}
                  disabled={isReadOnlySourceNode}
                  style={{ ...TEXTAREA_STYLE, fontFamily: 'monospace', minHeight: '100px' }}
                  placeholder="^\\s*([A-Za-z])\\s*$"
                />
              </div>
            </>
          )}

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-temperature" style={LABEL_STYLE}>
              Temperature
            </label>
            <input
              id="config-temperature"
              type="number"
              step="0.1"
              min="0"
              max="2"
              value={localConfig.temperature ?? ''}
              onChange={(e) => handleConfigChange('temperature', parseFloat(e.target.value))}
              disabled={isReadOnlySourceNode}
              style={INPUT_STYLE}
              placeholder="0.0 - 2.0"
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-max-tokens" style={LABEL_STYLE}>
              Max Tokens
            </label>
            <input
              id="config-max-tokens"
              type="number"
              value={localConfig.maxTokens ?? ''}
              onChange={(e) => handleConfigChange('maxTokens', parseInt(e.target.value, 10))}
              disabled={isReadOnlySourceNode}
              style={INPUT_STYLE}
              placeholder="Max output tokens"
            />
          </div>

          <RetryPolicySection config={localConfig} onChange={handleConfigChange} disabled={isReadOnlySourceNode} />
        </>
      )}

      {selectedNode.data.type === 'agent_run' && (
        <>
          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-agent-run-harness" style={LABEL_STYLE}>
              Harness
            </label>
            <select
              id="config-agent-run-harness"
              value={localConfig.harness || 'claude-code'}
              onChange={(e) => handleConfigChange('harness', e.target.value)}
              style={SELECT_STYLE}
            >
              <option value="claude-code">Claude Code</option>
              <option value="codex">Codex</option>
            </select>
          </div>

          <div style={{ marginBottom: '15px' }}>
            <SandboxSelect id="config-agent-run-sandbox" config={localConfig} onChange={handleConfigChange} />
          </div>

          <NovomoHandoffSection
            config={localConfig}
            onChange={handleConfigChange}
            nodes={nodes}
            edges={edges}
            selectedNodeId={selectedNodeId!}
          />

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-agent-run-prompt" style={LABEL_STYLE}>
              Prompt
            </label>
            <textarea
              id="config-agent-run-prompt"
              value={localConfig.prompt || ''}
              onChange={(e) => handleConfigChange('prompt', e.target.value)}
              style={{
                ...TEXTAREA_STYLE,
                fontSize: '14px',
                fontFamily: 'inherit',
                minHeight: '120px',
              }}
              placeholder="Delegate a concrete task to Novomo. Use {{variable}} for workflow context."
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-agent-run-timeout" style={LABEL_STYLE}>
              Timeout (seconds)
            </label>
            <input
              id="config-agent-run-timeout"
              type="number"
              min={1}
              value={localConfig.timeoutSeconds ?? 600}
              onChange={(e) => handleConfigChange('timeoutSeconds', parseInt(e.target.value, 10))}
              style={INPUT_STYLE}
              placeholder="600"
            />
          </div>

          <RetryPolicySection config={localConfig} onChange={handleConfigChange} />
        </>
      )}

      {selectedNode.data.type === 'novo_run' && (
        <>
          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-novo-run-goal" style={LABEL_STYLE}>
              Goal
            </label>
            <textarea
              id="config-novo-run-goal"
              value={localConfig.prompt || ''}
              onChange={(e) => handleConfigChange('prompt', e.target.value)}
              style={{
                ...TEXTAREA_STYLE,
                fontSize: '14px',
                fontFamily: 'inherit',
                minHeight: '120px',
              }}
              placeholder="Wake Novo with a concrete objective. Use {{variable}} for workflow context."
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-novo-task-id" style={LABEL_STYLE}>
              Existing Task ID
            </label>
            <input
              id="config-novo-task-id"
              type="text"
              value={localConfig.taskId || ''}
              onChange={(e) => handleConfigChange('taskId', e.target.value)}
              style={INPUT_STYLE}
              placeholder="task-..."
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-novo-task-summary" style={LABEL_STYLE}>
              Task Summary
            </label>
            <input
              id="config-novo-task-summary"
              type="text"
              value={localConfig.taskSummary || ''}
              onChange={(e) => handleConfigChange('taskSummary', e.target.value)}
              style={INPUT_STYLE}
              placeholder="Short task summary"
            />
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '15px' }}>
            <div>
              <label htmlFor="config-novo-identity" style={LABEL_STYLE}>
                Identity
              </label>
              <input
                id="config-novo-identity"
                type="text"
                value={localConfig.identity || 'sde-novo'}
                onChange={(e) => handleConfigChange('identity', e.target.value)}
                style={INPUT_STYLE}
                placeholder="sde-novo"
              />
            </div>
            <div>
              <SandboxSelect id="config-novo-sandbox" config={localConfig} onChange={handleConfigChange} />
            </div>
          </div>

          <NovomoHandoffSection
            config={localConfig}
            onChange={handleConfigChange}
            nodes={nodes}
            edges={edges}
            selectedNodeId={selectedNodeId!}
          />

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-novo-image" style={LABEL_STYLE}>
              Image
            </label>
            <input
              id="config-novo-image"
              type="text"
              value={localConfig.image || ''}
              onChange={(e) => handleConfigChange('image', e.target.value)}
              style={INPUT_STYLE}
              placeholder="Novomo default"
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-novo-runtime-url" style={LABEL_STYLE}>
              Runtime URL
            </label>
            <input
              id="config-novo-runtime-url"
              type="text"
              value={localConfig.runtimeUrl || ''}
              onChange={(e) => handleConfigChange('runtimeUrl', e.target.value)}
              style={INPUT_STYLE}
              placeholder="Novomo default"
            />
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '15px' }}>
            <div>
              <label htmlFor="config-novo-timeout" style={LABEL_STYLE}>
                Timeout (seconds)
              </label>
              <input
                id="config-novo-timeout"
                type="number"
                min={1}
                value={localConfig.timeoutSeconds ?? 600}
                onChange={(e) => handleConfigChange('timeoutSeconds', parseInt(e.target.value, 10))}
                style={INPUT_STYLE}
                placeholder="600"
              />
            </div>
            <div>
              <label htmlFor="config-novo-grace" style={LABEL_STYLE}>
                Grace (seconds)
              </label>
              <input
                id="config-novo-grace"
                type="number"
                min={0}
                value={localConfig.graceSeconds ?? 10}
                onChange={(e) => handleConfigChange('graceSeconds', parseInt(e.target.value, 10))}
                style={INPUT_STYLE}
                placeholder="10"
              />
            </div>
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-novo-repo-specs" style={LABEL_STYLE}>
              Repo Specs JSON
            </label>
            <textarea
              id="config-novo-repo-specs"
              value={localConfig.repoSpecsJson || ''}
              onChange={(e) => handleConfigChange('repoSpecsJson', e.target.value)}
              style={{ ...TEXTAREA_STYLE, fontFamily: 'monospace', minHeight: '90px' }}
              placeholder='[{"name":"app","source":{"type":"host_path","host_path":{"path":"/tmp/app"}}}]'
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-novo-work-source" style={LABEL_STYLE}>
              Work Source JSON
            </label>
            <textarea
              id="config-novo-work-source"
              value={localConfig.workSourceJson || ''}
              onChange={(e) => handleConfigChange('workSourceJson', e.target.value)}
              style={{ ...TEXTAREA_STYLE, fontFamily: 'monospace', minHeight: '90px' }}
              placeholder='{"type":"gitea_branch","gitea_branch":{"branch_ref":"main"}}'
            />
          </div>

          <RetryPolicySection config={localConfig} onChange={handleConfigChange} />
        </>
      )}

      {selectedNode.data.type === 'operation' && (
        <>
          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-operation-type" style={LABEL_STYLE}>
              Operation Type
            </label>
            <select
              id="config-operation-type"
              value={localConfig.operationType || 'collect_inputs'}
              onChange={(e) => handleConfigChange('operationType', e.target.value)}
              disabled={isReadOnlySourceNode}
              style={SELECT_STYLE}
            >
              {OPERATION_TYPES.map((operationType) => (
                <option key={operationType} value={operationType}>
                  {operationType}
                </option>
              ))}
            </select>
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-operation-config" style={LABEL_STYLE}>
              Operation Config JSON
            </label>
            <textarea
              id="config-operation-config"
              value={operationConfigText}
              onChange={(e) => {
                setOperationConfigText(e.target.value);
                setOperationConfigError(null);
              }}
              onBlur={(e) => {
                if (isReadOnlySourceNode) return;
                try {
                  const parsed = JSON.parse(e.target.value || '{}');
                  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                    throw new Error('Operation config must be a JSON object');
                  }
                  handleConfigChange('operationConfig', parsed);
                  setOperationConfigText(JSON.stringify(parsed, null, 2));
                  setOperationConfigError(null);
                } catch (error) {
                  setOperationConfigError(
                    error instanceof Error ? error.message : 'Operation config must be valid JSON',
                  );
                }
              }}
              disabled={isReadOnlySourceNode}
              style={{ ...TEXTAREA_STYLE, fontFamily: 'monospace', minHeight: '140px' }}
              placeholder='{"input_ids":["agent-a","agent-b"]}'
            />
            {operationConfigError && (
              <div style={{ color: '#b91c1c', fontSize: '12px', marginTop: '4px' }}>{operationConfigError}</div>
            )}
          </div>
        </>
      )}

      {selectedNode.data.type === 'workflow_ref' && (
        <>
          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-workflow-ref-id" style={LABEL_STYLE}>
              Use Workflow
            </label>
            <div style={HELP_TEXT_STYLE}>Compile this workflow into the current DAG before execution</div>
            <select
              id="config-workflow-ref-id"
              value={localConfig.workflowRefId || localConfig.workflowId || ''}
              onChange={(e) => handleWorkflowRefIdChange(e.target.value)}
              disabled={isReadOnlySourceNode}
              style={SELECT_STYLE}
            >
              <option value="">-- Select workflow --</option>
              {availableWorkflows.map((wf) => (
                <option key={wf.id} value={wf.id}>
                  {wf.name} ({wf.id})
                </option>
              ))}
            </select>
          </div>

          <InputTemplateEditor
            value={localConfig.inputTemplate}
            resetKey={`${selectedNode.id}:workflow_ref`}
            disabled={isReadOnlySourceNode}
            onChange={(value) => handleConfigChange('inputTemplate', value)}
            description={
              <>
                Map current workflow variables to referenced workflow input keys. Values use{' '}
                <code style={{ background: '#e8e8e8', padding: '1px 4px', borderRadius: '3px' }}>{'{{variable}}'}</code>{' '}
                syntax.
              </>
            }
          />

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-workflow-ref-output-key" style={LABEL_STYLE}>
              Output Key
            </label>
            <div style={HELP_TEXT_STYLE}>Optional output node or output name to expose downstream</div>
            <input
              id="config-workflow-ref-output-key"
              type="text"
              value={localConfig.outputKey || ''}
              onChange={(e) => handleConfigChange('outputKey', e.target.value)}
              disabled={isReadOnlySourceNode}
              style={{ ...INPUT_STYLE, fontFamily: 'monospace' }}
              placeholder="e.g., result"
            />
          </div>
        </>
      )}

      {selectedNode.data.type === 'child_workflow' && (
        <>
          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-child-workflow-id" style={LABEL_STYLE}>
              Run Workflow As Separate Job
            </label>
            <div style={HELP_TEXT_STYLE}>Select which workflow to execute as a separate durable child job</div>
            <select
              id="config-child-workflow-id"
              value={localConfig.childWorkflowId || ''}
              onChange={(e) => handleConfigChange('childWorkflowId', e.target.value)}
              style={SELECT_STYLE}
            >
              <option value="">-- Select workflow --</option>
              {availableWorkflows.map((wf) => (
                <option key={wf.id} value={wf.id}>
                  {wf.name} ({wf.id})
                </option>
              ))}
            </select>
          </div>

          <InputTemplateEditor
            value={localConfig.childInputTemplate}
            resetKey={`${selectedNode.id}:child_workflow`}
            disabled={isReadOnlySourceNode}
            onChange={(value) => handleConfigChange('childInputTemplate', value)}
            description={
              <>
                Map parent context variables to child input keys. Values use{' '}
                <code style={{ background: '#e8e8e8', padding: '1px 4px', borderRadius: '3px' }}>{'{{variable}}'}</code>{' '}
                syntax.
              </>
            }
          />

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-child-output-key" style={LABEL_STYLE}>
              Output Key
            </label>
            <div style={HELP_TEXT_STYLE}>Which key from the child workflow output to capture</div>
            <input
              id="config-child-output-key"
              type="text"
              value={localConfig.childOutputKey || ''}
              onChange={(e) => handleConfigChange('childOutputKey', e.target.value)}
              style={{ ...INPUT_STYLE, fontFamily: 'monospace' }}
              placeholder="e.g., synthesized_response"
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label htmlFor="config-child-timeout" style={LABEL_STYLE}>
              Timeout (seconds)
            </label>
            <input
              id="config-child-timeout"
              type="number"
              min={0}
              value={localConfig.timeoutSeconds ?? ''}
              onChange={(e) => handleConfigChange('timeoutSeconds', parseInt(e.target.value, 10))}
              style={INPUT_STYLE}
              placeholder="0 = no timeout"
            />
          </div>

          <div style={{ marginBottom: '15px' }}>
            <label style={LABEL_STYLE}>
              <input
                type="checkbox"
                checked={localConfig.childAwait !== false}
                onChange={(e) => handleConfigChange('childAwait', e.target.checked)}
                style={{ marginRight: '8px' }}
              />
              Await child completion
            </label>
            <div style={HELP_TEXT_STYLE}>When checked, parent waits for child workflow to finish before continuing</div>
          </div>

          <RetryPolicySection config={localConfig} onChange={handleConfigChange} />
        </>
      )}

      {selectedNode.data.type === 'conditional' && (
        <div style={{ marginBottom: '15px' }}>
          <label htmlFor="config-condition" style={LABEL_STYLE}>
            Condition
          </label>
          <textarea
            id="config-condition"
            value={localConfig.condition || ''}
            onChange={(e) => handleConfigChange('condition', e.target.value)}
            style={{
              ...TEXTAREA_STYLE,
              fontSize: '14px',
              fontFamily: 'inherit',
              minHeight: '80px',
            }}
            placeholder="JavaScript expression (e.g., input.score > 0.8)"
          />
        </div>
      )}

      {selectedNode.data.type === 'result' && (
        <div style={{ marginBottom: '15px' }}>
          <label htmlFor="config-output-format" style={LABEL_STYLE}>
            Output Format
          </label>
          <select
            id="config-output-format"
            value={localConfig.outputFormat || 'text'}
            onChange={(e) => handleConfigChange('outputFormat', e.target.value)}
            style={SELECT_STYLE}
          >
            <option value="text">Text</option>
            <option value="json">JSON</option>
            <option value="structured">Structured</option>
          </select>
        </div>
      )}

      {selectedNode.data.type === 'aggregation' && (
        <div style={{ marginBottom: '15px' }}>
          <label htmlFor="config-aggregation-workflow-id" style={LABEL_STYLE}>
            Aggregation Source Workflow
          </label>
          <div style={HELP_TEXT_STYLE}>
            Use a canonical L0 aggregation workflow as the compiled implementation for this collapsed node.
          </div>
          <select
            id="config-aggregation-workflow-id"
            value={aggregationWorkflowId}
            onChange={(e) => handleAggregationWorkflowChange(e.target.value)}
            disabled={isReadOnlySourceNode}
            style={{
              ...SELECT_STYLE,
              opacity: isReadOnlySourceNode ? 0.75 : 1,
            }}
          >
            <option value="">-- Select aggregation workflow --</option>
            {aggregationWorkflowOptions.map((wf) => (
              <option key={wf.id} value={wf.id}>
                {wf.name} ({wf.id})
              </option>
            ))}
          </select>

          {aggregationWorkflowOptions.length === 0 && (
            <div style={{ ...HELP_TEXT_STYLE, marginTop: '6px', color: '#b45309' }}>
              No L0 aggregation workflows are available in the current workflow list.
            </div>
          )}

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '8px', marginTop: '10px' }}>
            <button
              type="button"
              data-testid="aggregation-expand-button"
              onClick={handleExpandAggregation}
              disabled={!aggregationWorkflowId || aggregationActionLoading}
              style={{
                padding: '8px',
                borderRadius: '4px',
                border: '1px solid #0d9488',
                backgroundColor: '#0f766e',
                color: 'white',
                cursor: !aggregationWorkflowId || aggregationActionLoading ? 'not-allowed' : 'pointer',
                opacity: !aggregationWorkflowId || aggregationActionLoading ? 0.65 : 1,
              }}
            >
              {expandedAggregationVisible ? 'Refresh' : 'Expand'}
            </button>
            <button
              type="button"
              data-testid="aggregation-collapse-button"
              onClick={() => selectedNodeId && collapseAggregationNode(selectedNodeId)}
              disabled={!expandedAggregationVisible}
              style={{
                padding: '8px',
                borderRadius: '4px',
                border: '1px solid #cbd5e1',
                backgroundColor: '#f8fafc',
                color: '#334155',
                cursor: expandedAggregationVisible ? 'pointer' : 'not-allowed',
                opacity: expandedAggregationVisible ? 1 : 0.65,
              }}
            >
              Collapse
            </button>
            <button
              type="button"
              data-testid="aggregation-fork-button"
              onClick={handleForkAggregation}
              disabled={!aggregationWorkflowId || aggregationActionLoading}
              style={{
                padding: '8px',
                borderRadius: '4px',
                border: '1px solid #7c3aed',
                backgroundColor: '#6d28d9',
                color: 'white',
                cursor: !aggregationWorkflowId || aggregationActionLoading ? 'not-allowed' : 'pointer',
                opacity: !aggregationWorkflowId || aggregationActionLoading ? 0.65 : 1,
              }}
            >
              Fork
            </button>
          </div>

          {aggregationActionError && (
            <div style={{ color: '#b91c1c', fontSize: '12px', marginTop: '8px' }}>{aggregationActionError}</div>
          )}
        </div>
      )}

      {isAggregationConfigNode && (
        <div style={{ marginBottom: '15px' }}>
          <label htmlFor="config-aggregation-method" style={LABEL_STYLE}>
            Aggregation Method
          </label>
          <select
            id="config-aggregation-method"
            value={localConfig.aggregationMethod || 'collect'}
            onChange={(e) => handleAggregationMethodChange(e.target.value as AggregationMethod)}
            disabled={isReadOnlySourceNode}
            style={{
              ...SELECT_STYLE,
              opacity: isReadOnlySourceNode ? 0.75 : 1,
            }}
          >
            <option value="collect">Collect (Join with separator)</option>
            <option value="synthesis">Synthesis (LLM combines all)</option>
            <option value="judge">Judge (LLM picks best)</option>
            <option value="scoring">Scoring (Rubric-based)</option>
            <option value="peer_matrix">Peer Matrix (Agents score each other)</option>
            <option value="majority_vote">Majority Vote (Extract &amp; count answers)</option>
            <option value="debate_decide">Debate &amp; Decide (Camp adjudication)</option>
          </select>
        </div>
      )}

      {isAggregationConfigNode && localConfig.aggregationMethod && (
        <AggregationConfigSection
          method={localConfig.aggregationMethod}
          aggregationConfig={localConfig.aggregationConfig}
          models={models}
          loadingModels={loadingModels}
          disabled={isReadOnlySourceNode}
          onChange={(config) => handleConfigChange('aggregationConfig', config)}
        />
      )}

      {(selectedNode.data.type === 'input' || selectedNode.data.type === 'result') && (
        <div style={{ marginBottom: '15px' }}>
          <label htmlFor="config-schema" style={LABEL_STYLE}>
            Schema (JSON)
          </label>
          <textarea
            id="config-schema"
            value={schemaText}
            onChange={(e) => setSchemaText(e.target.value)}
            onBlur={(e) => {
              try {
                const parsed = JSON.parse(e.target.value);
                handleConfigChange(selectedNode.data.type === 'input' ? 'schema' : 'outputSchema', parsed);
              } catch {
                // Invalid JSON - revert to last valid state
                const schema = localConfig.schema || localConfig.outputSchema;
                setSchemaText(schema ? JSON.stringify(schema, null, 2) : '');
              }
            }}
            style={{
              ...TEXTAREA_STYLE,
              fontSize: '12px',
              minHeight: '120px',
            }}
            placeholder='{"topic": {"type": "text", "label": "Topic", "required": true}}'
          />
        </div>
      )}

      {!isReadOnlySourceNode && (
        <div style={{ marginTop: '30px', paddingTop: '20px', borderTop: '1px solid #ddd' }}>
          <button
            type="button"
            onClick={handleDelete}
            style={{
              padding: '10px',
              backgroundColor: '#f44336',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              fontSize: '14px',
              width: '100%',
            }}
          >
            Delete Node
          </button>
        </div>
      )}
    </div>
  );
};

export default ConfigPanel;
