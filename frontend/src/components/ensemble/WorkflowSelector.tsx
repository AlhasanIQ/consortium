import { useEffect, useState } from 'react';
import { useEnsembleStore } from '../../stores/ensembleStore';
import type { SeedWorkflowInfo } from '../../types/workflow';
import { extractAgentsFromWorkflow } from '../../utils/ensembleSync';

// Aggregation method color scheme
const aggregationColors: Record<string, string> = {
  judge: '#3B82F6', // Blue
  scoring: '#F59E0B', // Yellow/Amber
  synthesis: '#8B5CF6', // Purple
  peer_matrix: '#06B6D4', // Cyan
  collect: '#6B7280', // Gray
};

export function WorkflowSelector() {
  const { selectedWorkflowId, setSelectedWorkflow, availableWorkflows, setAvailableWorkflows, isExecuting } =
    useEnsembleStore();

  const [isOpen, setIsOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(false);

  // Fetch available workflows on mount
  useEffect(() => {
    async function fetchWorkflows() {
      try {
        const response = await fetch('/api/workflows/seeds');
        if (response.ok) {
          const data = await response.json();
          setAvailableWorkflows(data.seeds || []);
        }
      } catch (err) {
        console.warn('[WorkflowSelector] Failed to fetch seeds:', err);
      }
    }
    fetchWorkflows();
  }, [setAvailableWorkflows]);

  // Handle workflow selection
  const handleSelect = async (workflow: SeedWorkflowInfo) => {
    if (isExecuting) return;

    setIsLoading(true);
    setIsOpen(false);

    try {
      // Fetch the full workflow definition
      const response = await fetch(`/api/workflows/${workflow.id}`);
      if (response.ok) {
        const workflowDef = await response.json();

        // Extract agents from the workflow
        const agents = extractAgentsFromWorkflow(workflowDef);
        if (agents.length > 0) {
          // Update the store with the new workflow and agents
          setSelectedWorkflow(workflow.id);
          useEnsembleStore.setState({ agents });
          console.log('[WorkflowSelector] Loaded workflow:', workflow.name, 'with', agents.length, 'agents');
        }
      }
    } catch (err) {
      console.error('[WorkflowSelector] Failed to load workflow:', err);
    } finally {
      setIsLoading(false);
    }
  };

  const selectedWorkflow = availableWorkflows.find((w) => w.id === selectedWorkflowId);

  // Ensemble only exposes L1 reasoning primitives. L0 aggregation internals,
  // L2 composites, and L3 benchmark harnesses are not directly runnable here.
  const selectableWorkflows = availableWorkflows.filter((w) => w.layer === 'L1');

  return (
    <div style={{ position: 'relative' }}>
      {/* Selected workflow button */}
      <button
        type="button"
        onClick={() => !isExecuting && setIsOpen(!isOpen)}
        disabled={isExecuting || isLoading}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          padding: '6px 12px',
          backgroundColor: 'rgba(255, 255, 255, 0.1)',
          border: '1px solid rgba(255, 255, 255, 0.2)',
          borderRadius: '8px',
          color: '#fff',
          cursor: isExecuting ? 'not-allowed' : 'pointer',
          fontSize: '13px',
          opacity: isExecuting ? 0.5 : 1,
          transition: 'all 0.2s',
        }}
      >
        {isLoading ? (
          <span style={{ opacity: 0.7 }}>Loading...</span>
        ) : (
          <>
            <span style={{ fontWeight: 500 }}>{selectedWorkflow?.name || 'Select Workflow'}</span>
            {selectedWorkflow && (
              <span
                style={{
                  fontSize: '10px',
                  padding: '2px 6px',
                  borderRadius: '4px',
                  backgroundColor: aggregationColors[selectedWorkflow.aggregation_method] || '#6B7280',
                  color: '#fff',
                  textTransform: 'uppercase',
                  letterSpacing: '0.5px',
                }}
              >
                {selectedWorkflow.aggregation_method.replace('_', ' ')}
              </span>
            )}
            <span style={{ marginLeft: '4px', opacity: 0.6 }}>▼</span>
          </>
        )}
      </button>

      {/* Dropdown menu */}
      {isOpen && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            marginTop: '4px',
            minWidth: '280px',
            backgroundColor: '#1f1f1f',
            border: '1px solid rgba(255, 255, 255, 0.15)',
            borderRadius: '8px',
            boxShadow: '0 4px 20px rgba(0, 0, 0, 0.5)',
            zIndex: 1000,
            maxHeight: 'min(420px, calc(100vh - 120px))',
            overflowY: 'auto',
            overscrollBehavior: 'contain',
          }}
        >
          {selectableWorkflows.map((workflow) => (
            <button
              key={workflow.id}
              type="button"
              onClick={() => handleSelect(workflow)}
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'flex-start',
                gap: '4px',
                width: '100%',
                padding: '12px 16px',
                backgroundColor: workflow.id === selectedWorkflowId ? 'rgba(139, 92, 246, 0.2)' : 'transparent',
                border: 'none',
                borderBottom: '1px solid rgba(255, 255, 255, 0.1)',
                color: '#fff',
                cursor: 'pointer',
                textAlign: 'left',
                transition: 'background-color 0.15s',
              }}
              onMouseEnter={(e) => {
                if (workflow.id !== selectedWorkflowId) {
                  e.currentTarget.style.backgroundColor = 'rgba(255, 255, 255, 0.05)';
                }
              }}
              onMouseLeave={(e) => {
                if (workflow.id !== selectedWorkflowId) {
                  e.currentTarget.style.backgroundColor = 'transparent';
                }
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <span style={{ fontWeight: 500, fontSize: '14px' }}>{workflow.name}</span>
                <span
                  style={{
                    fontSize: '9px',
                    padding: '2px 5px',
                    borderRadius: '3px',
                    backgroundColor: aggregationColors[workflow.aggregation_method] || '#6B7280',
                    color: '#fff',
                    textTransform: 'uppercase',
                    letterSpacing: '0.5px',
                  }}
                >
                  {workflow.aggregation_method.replace('_', ' ')}
                </span>
                <span
                  style={{
                    fontSize: '11px',
                    color: 'rgba(255, 255, 255, 0.5)',
                  }}
                >
                  {workflow.agent_count} agents
                </span>
              </div>
              <span
                style={{
                  fontSize: '12px',
                  color: 'rgba(255, 255, 255, 0.6)',
                  lineHeight: 1.4,
                }}
              >
                {workflow.description}
              </span>
            </button>
          ))}
        </div>
      )}

      {/* Click outside to close */}
      {isOpen && (
        <div
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 999,
          }}
          onClick={() => setIsOpen(false)}
          onKeyDown={(e) => e.key === 'Escape' && setIsOpen(false)}
        />
      )}
    </div>
  );
}
