import type React from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useJobConfig } from '../../hooks/useJobConfig';
import { useJobTraces } from '../../hooks/useJobTraces';
import type { ReplayJobInfo, ReplayNodeResult } from '../../stores/workflowStore';
import type { TraceSpan } from '../../types/events';
import {
  buildFlatReplayNodes,
  computeReplayTotals,
  deriveGroupStatus,
  extractTraceEvents,
  formatCost,
  formatDuration,
  formatLatency,
  formatMetadata,
  formatNumber,
  getBaseNodeId,
  getReplayStatusColor,
  groupReplayNodes,
  kindColors,
  parsePromptMessages,
  type ReplayNodeGroup,
  traceStatusColors,
} from './executionPanelUtils';
import RunConfigPanel from './RunConfigPanel';
import TimelineScrubber from './TimelineScrubber';

interface ReplayTabProps {
  replayJobId: string | null;
  replayJob: ReplayJobInfo | null;
  replayNodes: ReplayNodeResult[];
  replayNodeResults: Record<string, ReplayNodeResult>;
  replaySelectedNodeId: string | null;
  requestReplayFocus: (nodeId: string) => void;
  nodeIdSet: Set<string>;
  selectedBaseId: string | null;
}

const TraceSpanView: React.FC<{ span: TraceSpan }> = ({ span }) => {
  const isChild = !!span.parent_span_id;
  const attrs = span.attributes || {};
  return (
    <div
      style={{
        padding: '6px',
        backgroundColor: 'white',
        borderRadius: '4px',
        border: '1px solid #e0e0e0',
        marginLeft: isChild ? '12px' : '0',
        borderLeft: `3px solid ${kindColors[span.kind] || '#999'}`,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '6px',
          flexWrap: 'wrap',
        }}
      >
        <span
          style={{
            padding: '1px 5px',
            borderRadius: '3px',
            backgroundColor: kindColors[span.kind] || '#999',
            color: 'white',
            fontSize: '9px',
            fontWeight: 600,
            textTransform: 'uppercase',
          }}
        >
          {span.kind}
        </span>
        <span
          style={{
            padding: '1px 5px',
            borderRadius: '3px',
            backgroundColor: traceStatusColors[span.status] || '#999',
            color: 'white',
            fontSize: '9px',
          }}
        >
          {span.status}
        </span>
        {span.duration_ms != null && (
          <span style={{ fontSize: '9px', color: '#666' }}>{formatLatency(span.duration_ms)}</span>
        )}
        {typeof attrs.call_type === 'string' && (
          <span style={{ fontSize: '9px', color: '#555', fontWeight: 600 }}>{attrs.call_type}</span>
        )}
        {typeof attrs.decision_type === 'string' && (
          <span style={{ fontSize: '9px', color: '#555', fontWeight: 600 }}>{attrs.decision_type}</span>
        )}
      </div>
      <div style={{ marginTop: '4px', fontSize: '9px', color: '#666' }}>
        {typeof attrs.model === 'string' && <div>Model: {attrs.model}</div>}
        {typeof attrs.tokens_input === 'number' && (
          <div>
            Tokens: {formatNumber(attrs.tokens_input as number)}
            {' -> '}
            {formatNumber(attrs.tokens_output as number)} | Cost: {formatCost(attrs.cost as number)}
          </div>
        )}
        {typeof attrs.finish_reason === 'string' && <div>Finish: {attrs.finish_reason}</div>}
        {typeof attrs.result === 'string' && <div>Result: {attrs.result}</div>}
        {typeof attrs.branch_taken === 'string' && <div>Branch: {attrs.branch_taken}</div>}
        {typeof attrs.condition === 'string' && <div>Condition: {attrs.condition}</div>}
        {typeof attrs.error === 'string' && <div style={{ color: '#c62828' }}>Error: {attrs.error}</div>}
        {typeof attrs.method === 'string' && <div>Method: {attrs.method}</div>}
        {typeof attrs.winner === 'string' && <div>Winner: {attrs.winner}</div>}
      </div>
    </div>
  );
};

const NodeDetailView: React.FC<{
  activeNode: ReplayNodeResult;
  hasNode: boolean;
  requestReplayFocus: (nodeId: string) => void;
  nodeTraceSpans: TraceSpan[];
  traceEvents: { type?: string; name?: string; label?: string; timestamp?: string; data?: unknown }[];
  promptMessages: { role: string; content: string }[];
  duration: string | null;
}> = ({ activeNode, hasNode, requestReplayFocus, nodeTraceSpans, traceEvents, promptMessages, duration }) => {
  return (
    <div
      style={{
        marginTop: '10px',
        paddingTop: '10px',
        borderTop: '1px solid #e0e0e0',
        display: 'flex',
        flexDirection: 'column',
        gap: '8px',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ fontSize: '11px', fontWeight: 600, color: '#333' }}>Node Details</div>
        {hasNode && (
          <button
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              requestReplayFocus(activeNode.node_id);
            }}
            style={{
              padding: '4px 8px',
              fontSize: '10px',
              backgroundColor: '#1976d2',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Focus on canvas
          </button>
        )}
      </div>
      <div style={{ fontSize: '10px', color: '#555' }}>
        Status: {activeNode.status} • Latency: {formatLatency(activeNode.latency_ms)}
        {activeNode.attempt_number && activeNode.attempt_number > 1 ? ` • Attempt ${activeNode.attempt_number}` : ''}
      </div>
      {duration && <div style={{ fontSize: '10px', color: '#555' }}>Duration: {duration}</div>}
      <div style={{ fontSize: '10px', color: '#555' }}>
        Tokens: {formatNumber(activeNode.tokens_input)}
        {' -> '}
        {formatNumber(activeNode.tokens_output)} • Cost: {formatCost(activeNode.cost)}
      </div>
      {activeNode.model && <div style={{ fontSize: '10px', color: '#555' }}>Model: {activeNode.model}</div>}
      {activeNode.execution_uid && (
        <div style={{ fontSize: '10px', color: '#555' }}>Execution UID: {activeNode.execution_uid}</div>
      )}

      {promptMessages.length > 0 && (
        <div
          style={{
            padding: '8px',
            backgroundColor: '#f7f7f7',
            borderRadius: '6px',
            fontSize: '11px',
            color: '#333',
          }}
        >
          <div style={{ fontWeight: 600, marginBottom: '6px' }}>Prompt</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
            {promptMessages.map((msg, msgIndex) => (
              <div
                key={`${msg.role}-${msgIndex}`}
                style={{
                  padding: '6px',
                  backgroundColor: 'white',
                  borderRadius: '4px',
                  border: '1px solid #e0e0e0',
                }}
              >
                <div
                  style={{
                    fontSize: '10px',
                    fontWeight: 600,
                    color: '#666',
                    textTransform: 'uppercase',
                    marginBottom: '4px',
                  }}
                >
                  {msg.role}
                </div>
                <div
                  style={{
                    fontFamily: 'monospace',
                    fontSize: '10px',
                    whiteSpace: 'pre-wrap',
                  }}
                >
                  {msg.content}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {activeNode.output && (
        <div
          style={{
            padding: '8px',
            backgroundColor: '#f7f7f7',
            borderRadius: '6px',
            fontSize: '11px',
            color: '#333',
            maxHeight: '180px',
            overflowY: 'auto',
            whiteSpace: 'pre-wrap',
            fontFamily: 'monospace',
          }}
        >
          {activeNode.output}
        </div>
      )}
      {activeNode.error_message && (
        <div
          style={{
            padding: '8px',
            backgroundColor: '#ffebee',
            borderRadius: '6px',
            fontSize: '11px',
            color: '#c62828',
          }}
        >
          {activeNode.error_message}
        </div>
      )}
      {nodeTraceSpans.length > 0 && (
        <div
          style={{
            padding: '8px',
            backgroundColor: '#f1f5f9',
            borderRadius: '6px',
            fontSize: '10px',
            color: '#555',
          }}
        >
          <div style={{ fontWeight: 600, marginBottom: '6px' }}>Trace Spans ({nodeTraceSpans.length})</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
            {nodeTraceSpans.map((span) => (
              <TraceSpanView key={span.span_id} span={span} />
            ))}
          </div>
        </div>
      )}
      {nodeTraceSpans.length === 0 && traceEvents.length > 0 && (
        <div
          style={{
            padding: '8px',
            backgroundColor: '#f1f5f9',
            borderRadius: '6px',
            fontSize: '10px',
            color: '#555',
          }}
        >
          <div style={{ fontWeight: 600, marginBottom: '6px' }}>Trace Events</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
            {traceEvents.map((event, eventIndex) => (
              <div
                key={`${event.type || event.name || 'event'}-${eventIndex}`}
                style={{
                  padding: '6px',
                  backgroundColor: 'white',
                  borderRadius: '4px',
                  border: '1px solid #e0e0e0',
                }}
              >
                <div style={{ fontSize: '10px', fontWeight: 600, color: '#333' }}>
                  {event.label || event.type || event.name || 'event'}
                </div>
                {event.timestamp && (
                  <div style={{ fontSize: '9px', color: '#777', marginTop: '2px' }}>
                    {new Date(event.timestamp).toLocaleString()}
                  </div>
                )}
                {event.data !== undefined && (
                  <div
                    style={{
                      fontFamily: 'monospace',
                      fontSize: '9px',
                      whiteSpace: 'pre-wrap',
                      marginTop: '4px',
                    }}
                  >
                    {JSON.stringify(event.data, null, 2)}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
      {activeNode.metadata && (
        <div
          style={{
            padding: '8px',
            backgroundColor: '#f3f3f3',
            borderRadius: '6px',
            fontSize: '10px',
            color: '#555',
            whiteSpace: 'pre-wrap',
            fontFamily: 'monospace',
          }}
        >
          {formatMetadata(activeNode.metadata)}
        </div>
      )}
    </div>
  );
};

const ReplayGroupCard: React.FC<{
  group: ReplayNodeGroup;
  index: number;
  isSelectedGroup: boolean;
  activeNode: ReplayNodeResult | undefined;
  hasNode: boolean;
  showChildren: boolean;
  replaySelectedNodeId: string | null;
  requestReplayFocus: (nodeId: string) => void;
  setExpandedGroups: React.Dispatch<React.SetStateAction<Record<string, boolean>>>;
  nodeTraceSpans: TraceSpan[];
  replayNodeResults: Record<string, ReplayNodeResult>;
}> = ({
  group,
  index,
  isSelectedGroup,
  activeNode,
  hasNode,
  showChildren,
  replaySelectedNodeId,
  requestReplayFocus,
  setExpandedGroups,
  nodeTraceSpans,
}) => {
  const groupNodes = group.primary ? [group.primary, ...group.children] : group.children;
  const groupStatus = deriveGroupStatus(groupNodes);
  const groupTotals = groupNodes.reduce(
    (acc, node) => {
      if (group.children.length > 0 && node.node_id === group.baseId) {
        return acc;
      }
      acc.tokensInput += node.tokens_input || 0;
      acc.tokensOutput += node.tokens_output || 0;
      acc.cost += node.cost || 0;
      acc.latency += node.latency_ms || 0;
      return acc;
    },
    { tokensInput: 0, tokensOutput: 0, cost: 0, latency: 0 },
  );

  const promptMessages = activeNode ? parsePromptMessages(activeNode.prompt) : [];
  const traceEvents = activeNode ? extractTraceEvents(activeNode.metadata) : [];
  const duration = activeNode ? formatDuration(activeNode.started_at, activeNode.completed_at) : null;
  const groupLabel = group.primary?.node_name || group.primary?.node_label || group.baseId;

  return (
    <div
      style={{
        backgroundColor: isSelectedGroup ? '#e3f2fd' : 'white',
        borderRadius: '8px',
        border: `1px solid ${isSelectedGroup ? '#90caf9' : '#e0e0e0'}`,
        padding: '10px',
      }}
    >
      <div
        role="button"
        tabIndex={0}
        onClick={() => {
          const focusNode = group.primary || group.children[0];
          if (focusNode) {
            requestReplayFocus(focusNode.node_id);
          }
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            const focusNode = group.primary || group.children[0];
            if (focusNode) {
              requestReplayFocus(focusNode.node_id);
            }
          }
        }}
        style={{ cursor: 'pointer' }}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <div
              style={{
                width: '8px',
                height: '8px',
                borderRadius: '50%',
                backgroundColor: getReplayStatusColor(groupStatus),
              }}
            />
            <div style={{ fontSize: '12px', fontWeight: 600, color: '#333' }}>{groupLabel}</div>
            <div style={{ fontSize: '10px', color: '#777', fontFamily: 'monospace' }}>{group.baseId}</div>
          </div>
          <div style={{ fontSize: '10px', color: '#999' }}>#{index + 1}</div>
        </div>
        <div style={{ fontSize: '10px', color: '#666', marginTop: '4px' }}>
          {(group.primary?.node_type || group.children[0]?.node_type || 'node').toString()} •{' '}
          {group.children.length > 0
            ? `${group.children.length} aggregation call${group.children.length > 1 ? 's' : ''}`
            : group.primary?.node_id || 'node'}
        </div>
        <div style={{ display: 'flex', gap: '10px', marginTop: '6px', fontSize: '10px', color: '#555' }}>
          <span>
            {formatNumber(groupTotals.tokensInput)}
            {' -> '}
            {formatNumber(groupTotals.tokensOutput)} tokens
          </span>
          <span>{formatCost(groupTotals.cost)}</span>
          <span>{formatLatency(groupTotals.latency)}</span>
        </div>
        {!hasNode && <div style={{ marginTop: '6px', fontSize: '10px', color: '#b26a00' }}>Not on canvas</div>}
      </div>

      {group.children.length > 0 && (
        <button
          type="button"
          onClick={() =>
            setExpandedGroups((prev) => ({
              ...prev,
              [group.baseId]: !prev[group.baseId],
            }))
          }
          style={{
            marginTop: '8px',
            padding: '4px 8px',
            fontSize: '10px',
            backgroundColor: '#f5f5f5',
            color: '#333',
            border: '1px solid #ddd',
            borderRadius: '4px',
            cursor: 'pointer',
          }}
        >
          {showChildren ? 'Hide subnodes' : `Show subnodes (${group.children.length})`}
        </button>
      )}

      {showChildren && (
        <div style={{ marginTop: '8px', display: 'flex', flexDirection: 'column', gap: '6px' }}>
          {group.children.map((child, childIndex) => {
            const isSelected = replaySelectedNodeId === child.node_id;
            return (
              <div
                key={child.node_id}
                role="button"
                tabIndex={0}
                onClick={() => requestReplayFocus(child.node_id)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    requestReplayFocus(child.node_id);
                  }
                }}
                style={{
                  padding: '8px',
                  borderRadius: '6px',
                  border: `1px solid ${isSelected ? '#90caf9' : '#e0e0e0'}`,
                  backgroundColor: isSelected ? '#e3f2fd' : '#fafafa',
                  cursor: 'pointer',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <div
                      style={{
                        width: '6px',
                        height: '6px',
                        borderRadius: '50%',
                        backgroundColor: getReplayStatusColor(child.status),
                      }}
                    />
                    <div style={{ fontSize: '11px', fontWeight: 600, color: '#333' }}>
                      {child.node_name || child.node_label || child.node_id}
                    </div>
                  </div>
                  <div style={{ fontSize: '10px', color: '#999' }}>{childIndex + 1}</div>
                </div>
                <div style={{ fontSize: '10px', color: '#666', marginTop: '4px' }}>
                  {child.node_type} • {child.model || 'no model'}
                </div>
                <div
                  style={{
                    display: 'flex',
                    gap: '10px',
                    marginTop: '4px',
                    fontSize: '10px',
                    color: '#555',
                  }}
                >
                  <span>
                    {formatNumber(child.tokens_input)}
                    {' -> '}
                    {formatNumber(child.tokens_output)} tokens
                  </span>
                  <span>{formatCost(child.cost)}</span>
                  <span>{formatLatency(child.latency_ms)}</span>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {isSelectedGroup && activeNode && (
        <NodeDetailView
          activeNode={activeNode}
          hasNode={hasNode}
          requestReplayFocus={requestReplayFocus}
          nodeTraceSpans={nodeTraceSpans}
          traceEvents={traceEvents}
          promptMessages={promptMessages}
          duration={duration}
        />
      )}
    </div>
  );
};

export const ReplayTab: React.FC<ReplayTabProps> = ({
  replayJobId,
  replayJob,
  replayNodes,
  replayNodeResults,
  replaySelectedNodeId,
  requestReplayFocus,
  nodeIdSet,
  selectedBaseId,
}) => {
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({});
  const [scrubberIndex, setScrubberIndex] = useState(0);
  const [searchInput, setSearchInput] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  const [searchScope, setSearchScope] = useState<'All' | 'Prompts' | 'Outputs' | 'Errors'>('All');

  const groupedReplayNodes = useMemo(() => groupReplayNodes(replayNodes), [replayNodes]);
  const flatReplayNodes = useMemo(() => buildFlatReplayNodes(groupedReplayNodes), [groupedReplayNodes]);
  const replayTotals = useMemo(() => computeReplayTotals(replayJob, replayNodes), [replayJob, replayNodes]);

  const missingBaseNodes = useMemo(() => {
    const missing = new Set<string>();
    replayNodes.forEach((node) => {
      const baseId = getBaseNodeId(node.node_id);
      if (!nodeIdSet.has(baseId)) {
        missing.add(baseId);
      }
    });
    return Array.from(missing);
  }, [replayNodes, nodeIdSet]);

  const { traceGroups } = useJobTraces(replayJobId);
  const { config: replayConfig } = useJobConfig(replayJobId);

  const tracesByNodeId = useMemo(() => {
    const map = new Map<string, TraceSpan[]>();
    for (const group of traceGroups) {
      map.set(group.node_id, group.spans);
    }
    return map;
  }, [traceGroups]);

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(searchInput);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchInput]);

  useEffect(() => {
    if (!replayJobId) {
      setSearchInput('');
      setDebouncedSearch('');
      setSearchScope('All');
    }
  }, [replayJobId]);

  const filteredReplayGroups = useMemo(() => {
    if (!debouncedSearch) return groupedReplayNodes;
    const query = debouncedSearch.toLowerCase();

    return groupedReplayNodes.filter((group) => {
      const allNodes = group.primary ? [group.primary, ...group.children] : group.children;
      return allNodes.some((node) => {
        if (searchScope === 'Prompts') {
          return node.prompt?.toLowerCase().includes(query) ?? false;
        }
        if (searchScope === 'Outputs') {
          return node.output?.toLowerCase().includes(query) ?? false;
        }
        if (searchScope === 'Errors') {
          return node.error_message?.toLowerCase().includes(query) ?? false;
        }
        const fields = [node.prompt, node.output, node.error_message, node.node_name, node.node_label, node.node_id];
        return fields.some((field) => field?.toLowerCase().includes(query));
      });
    });
  }, [groupedReplayNodes, debouncedSearch, searchScope]);

  const matchCount = debouncedSearch ? filteredReplayGroups.length : -1;

  const handleExportJson = useCallback(() => {
    const exportData: Record<string, unknown> = {
      job: replayJob,
      nodes: replayNodes,
    };
    if (traceGroups.length > 0) {
      exportData.traces = traceGroups;
    }
    const json = JSON.stringify(exportData, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `consortium-run-${replayJobId}.json`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(url);
  }, [replayJob, replayNodes, replayJobId, traceGroups]);

  const handleScrubberIndexChange = useCallback(
    (index: number) => {
      const clampedIndex = Math.max(0, Math.min(index, flatReplayNodes.length - 1));
      setScrubberIndex(clampedIndex);
      const item = flatReplayNodes[clampedIndex];
      if (item) {
        if (item.isSubnode) {
          const baseId = getBaseNodeId(item.nodeId);
          setExpandedGroups((prev) => ({ ...prev, [baseId]: true }));
        }
        requestReplayFocus(item.nodeId);
      }
    },
    [flatReplayNodes, requestReplayFocus],
  );

  useEffect(() => {
    if (!replaySelectedNodeId) return;
    const matchIndex = flatReplayNodes.findIndex((item) => item.nodeId === replaySelectedNodeId);
    if (matchIndex >= 0 && matchIndex !== scrubberIndex) {
      setScrubberIndex(matchIndex);
    }
  }, [replaySelectedNodeId, flatReplayNodes, scrubberIndex]);

  const prevReplayJobIdRef = useRef(replayJobId);
  useEffect(() => {
    if (prevReplayJobIdRef.current !== replayJobId) {
      prevReplayJobIdRef.current = replayJobId;
      setScrubberIndex(0);
    }
  }, [replayJobId]);

  // Sync expanded groups when canvas selection changes
  useEffect(() => {
    if (!replaySelectedNodeId) return;
    const baseId = getBaseNodeId(replaySelectedNodeId);
    if (selectedBaseId !== baseId) return;
    setExpandedGroups((prev) => {
      if (prev[baseId]) return prev;
      return { ...prev, [baseId]: true };
    });
  }, [replaySelectedNodeId, selectedBaseId]);

  if (!replayJob) {
    return (
      <div style={{ flex: 1, overflowY: 'auto' }}>
        <div style={{ textAlign: 'center', padding: '20px', color: '#666' }}>Loading replay...</div>
      </div>
    );
  }

  return (
    <div style={{ flex: 1, overflowY: 'auto' }}>
      {replayConfig && <RunConfigPanel config={replayConfig} />}

      <div
        style={{
          backgroundColor: 'white',
          borderRadius: '8px',
          border: '1px solid #e0e0e0',
          padding: '12px',
          marginBottom: '12px',
        }}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '8px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <div style={{ fontSize: '12px', fontWeight: 'bold', color: '#333' }}>Replay Summary</div>
            <button
              type="button"
              onClick={handleExportJson}
              style={{
                padding: '2px 8px',
                fontSize: '10px',
                backgroundColor: '#1976d2',
                color: 'white',
                border: 'none',
                borderRadius: '4px',
                cursor: 'pointer',
              }}
            >
              Export JSON
            </button>
          </div>
          <span
            style={{
              padding: '2px 8px',
              borderRadius: '10px',
              backgroundColor: getReplayStatusColor(replayJob.status),
              color: 'white',
              fontSize: '10px',
              textTransform: 'uppercase',
            }}
          >
            {replayJob.status}
          </span>
        </div>
        <div style={{ marginTop: '8px', fontSize: '11px', color: '#666' }}>
          {new Date(replayJob.created_at).toLocaleString()}
        </div>
        {replayJob.description && (
          <div style={{ marginTop: '6px', fontSize: '12px', color: '#444' }}>{replayJob.description}</div>
        )}
        <div style={{ marginTop: '10px', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '6px' }}>
          <div style={{ fontSize: '11px', color: '#555' }}>Tokens: {formatNumber(replayTotals.tokensTotal)}</div>
          <div style={{ fontSize: '11px', color: '#555' }}>Cost: {formatCost(replayTotals.cost)}</div>
          <div style={{ fontSize: '11px', color: '#555' }}>Input: {formatNumber(replayTotals.tokensInput)}</div>
          <div style={{ fontSize: '11px', color: '#555' }}>Output: {formatNumber(replayTotals.tokensOutput)}</div>
        </div>
        {replayJob.result_text && (
          <div
            style={{
              marginTop: '10px',
              padding: '8px',
              backgroundColor: '#f7f7f7',
              borderRadius: '6px',
              fontSize: '11px',
              color: '#333',
              maxHeight: '140px',
              overflowY: 'auto',
              whiteSpace: 'pre-wrap',
            }}
          >
            {replayJob.result_text}
          </div>
        )}
      </div>

      <div
        style={{
          backgroundColor: 'white',
          borderRadius: '8px',
          border: '1px solid #e0e0e0',
          padding: '10px',
          marginBottom: '12px',
          display: 'flex',
          flexDirection: 'column',
          gap: '8px',
        }}
      >
        <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
          <input
            type="text"
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="Search nodes..."
            style={{
              flex: 1,
              padding: '5px 8px',
              fontSize: '11px',
              border: '1px solid #ccc',
              borderRadius: '4px',
              outline: 'none',
            }}
          />
          <select
            value={searchScope}
            onChange={(e) => setSearchScope(e.target.value as 'All' | 'Prompts' | 'Outputs' | 'Errors')}
            style={{
              padding: '5px 6px',
              fontSize: '11px',
              border: '1px solid #ccc',
              borderRadius: '4px',
              backgroundColor: 'white',
              color: '#333',
              cursor: 'pointer',
            }}
          >
            <option value="All">All</option>
            <option value="Prompts">Prompts</option>
            <option value="Outputs">Outputs</option>
            <option value="Errors">Errors</option>
          </select>
        </div>
        {matchCount >= 0 && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
            <span
              style={{
                padding: '2px 8px',
                fontSize: '10px',
                backgroundColor: matchCount > 0 ? '#e3f2fd' : '#ffebee',
                color: matchCount > 0 ? '#1976d2' : '#c62828',
                borderRadius: '10px',
                fontWeight: 600,
              }}
            >
              {matchCount} match{matchCount !== 1 ? 'es' : ''}
            </span>
            <button
              type="button"
              onClick={() => {
                setSearchInput('');
                setDebouncedSearch('');
              }}
              style={{
                padding: '2px 6px',
                fontSize: '10px',
                backgroundColor: '#f5f5f5',
                color: '#666',
                border: '1px solid #ddd',
                borderRadius: '4px',
                cursor: 'pointer',
              }}
            >
              Clear
            </button>
          </div>
        )}
      </div>

      <TimelineScrubber
        items={flatReplayNodes}
        currentIndex={scrubberIndex}
        onIndexChange={handleScrubberIndexChange}
      />

      {missingBaseNodes.length > 0 && (
        <div
          style={{
            backgroundColor: '#fff8e1',
            border: '1px solid #ffe082',
            borderRadius: '6px',
            padding: '10px',
            fontSize: '11px',
            color: '#8d6e63',
            marginBottom: '12px',
          }}
        >
          {missingBaseNodes.length} node{missingBaseNodes.length > 1 ? 's' : ''} no longer match the current canvas.
          Replay details are still available, but focusing those nodes on the canvas is disabled.
        </div>
      )}

      {filteredReplayGroups.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '20px', color: '#999' }}>
          {debouncedSearch ? 'No matching nodes found.' : 'No nodes recorded for this replay.'}
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          {filteredReplayGroups.map((group, index) => {
            const isSelectedGroup = selectedBaseId === group.baseId;
            const selectedNode =
              replaySelectedNodeId && replayNodeResults[replaySelectedNodeId]
                ? replayNodeResults[replaySelectedNodeId]
                : undefined;
            const activeNode =
              isSelectedGroup && selectedNode && getBaseNodeId(selectedNode.node_id) === group.baseId
                ? selectedNode
                : group.primary || group.children[0];
            const hasNode = nodeIdSet.has(group.baseId);
            const showChildren = group.children.length > 0 && (expandedGroups[group.baseId] || isSelectedGroup);
            const nodeTraceSpans = tracesByNodeId.get(group.baseId) || [];

            return (
              <ReplayGroupCard
                key={group.baseId}
                group={group}
                index={index}
                isSelectedGroup={isSelectedGroup}
                activeNode={activeNode}
                hasNode={hasNode}
                showChildren={showChildren}
                replaySelectedNodeId={replaySelectedNodeId}
                requestReplayFocus={requestReplayFocus}
                setExpandedGroups={setExpandedGroups}
                nodeTraceSpans={nodeTraceSpans}
                replayNodeResults={replayNodeResults}
              />
            );
          })}
        </div>
      )}
    </div>
  );
};
