import type { FlowEdge, FlowNode } from '../types/workflow';
import { filterEphemeralAggregationGraph } from './aggregationPlan';

const STORAGE_KEY = 'consortium_workflow_state';

export interface WorkflowStorageState {
  nodes: FlowNode[];
  edges: FlowEdge[];
  timestamp: number;
}

/**
 * Save workflow state to localStorage
 */
export const saveWorkflowToLocalStorage = (nodes: FlowNode[], edges: FlowEdge[]): void => {
  try {
    const filtered = filterEphemeralAggregationGraph({ nodes, edges });
    const state: WorkflowStorageState = {
      nodes: filtered.nodes as FlowNode[],
      edges: filtered.edges as FlowEdge[],
      timestamp: Date.now(),
    };
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch (error) {
    console.error('Failed to save workflow to localStorage:', error);
  }
};

/**
 * Load workflow state from localStorage
 */
export const loadWorkflowFromLocalStorage = (): WorkflowStorageState | null => {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (!stored) return null;

    const state = JSON.parse(stored) as WorkflowStorageState;

    // Validate the structure
    if (!state.nodes || !state.edges || !Array.isArray(state.nodes) || !Array.isArray(state.edges)) {
      console.warn('Invalid workflow state in localStorage, ignoring');
      return null;
    }

    const filtered = filterEphemeralAggregationGraph({ nodes: state.nodes, edges: state.edges });
    return {
      ...state,
      nodes: filtered.nodes as FlowNode[],
      edges: filtered.edges as FlowEdge[],
    };
  } catch (error) {
    console.error('Failed to load workflow from localStorage:', error);
    return null;
  }
};

/**
 * Clear workflow state from localStorage
 */
export const clearWorkflowFromLocalStorage = (): void => {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch (error) {
    console.error('Failed to clear workflow from localStorage:', error);
  }
};

/**
 * Check if there's a saved workflow in localStorage
 */
export const hasSavedWorkflow = (): boolean => {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return !!stored;
  } catch {
    return false;
  }
};
