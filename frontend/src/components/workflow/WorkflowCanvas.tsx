import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  type NodeTypes,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
} from '@xyflow/react';
import type React from 'react';
import { useCallback, useEffect, useRef } from 'react';
import '@xyflow/react/dist/style.css';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import type { ExecuteWorkflowRequest, ExecutionState } from '../../hooks/useWorkflowExecution';
import { useWorkflowStore } from '../../stores/workflowStore';
import type { NodeData, NodeType } from '../../types/workflow';
import { workflowNodeLayoutSize } from '../../utils/dagreLayout';
import AgentNode from './nodes/AgentNode';
import AggregationFrameNode from './nodes/AggregationFrameNode';
import AggregationNode from './nodes/AggregationNode';
import ChildWorkflowNode from './nodes/ChildWorkflowNode';
import ConditionalNode from './nodes/ConditionalNode';
import InputNode from './nodes/InputNode';
import OperationNode from './nodes/OperationNode';
import ResultNode from './nodes/ResultNode';
import ResultsOverlay from './ResultsOverlay';
import WorkflowToolbar from './WorkflowToolbar';

const nodeTypes: NodeTypes = {
  agent: AgentNode,
  agent_run: AgentNode,
  novo_run: AgentNode,
  contract_extract: AgentNode,
  workflow_ref: ChildWorkflowNode,
  child_workflow: ChildWorkflowNode,
  input: InputNode,
  aggregation: AggregationNode,
  aggregation_frame: AggregationFrameNode,
  operation: OperationNode,
  result: ResultNode,
  conditional: ConditionalNode,
};

const getReplayBaseNodeId = (nodeId: string): string => {
  if (nodeId.includes('__')) {
    return nodeId.split('__')[0] || nodeId;
  }
  if (nodeId.includes('--')) {
    return nodeId.split('--')[0] || nodeId;
  }
  return nodeId;
};

const FIT_VIEW_PADDING = 0.14;
const MIN_FIT_PADDING_PX = 80;

interface WorkflowCanvasProps {
  onExecute?: (request: ExecuteWorkflowRequest) => void;
  isExecuting?: boolean;
  workflowId?: string;
  workflowName?: string;
  workflowIsSaved?: boolean;
  sourceJobId?: string;
  onWorkflowSaved?: (id: string, name: string) => void;
  executionState?: ExecutionState;
}

// Internal component that has access to ReactFlow context
const WorkflowCanvasInternal: React.FC<WorkflowCanvasProps> = ({
  onExecute,
  isExecuting,
  workflowId,
  workflowName,
  workflowIsSaved,
  sourceJobId,
  onWorkflowSaved,
  executionState,
}) => {
  const {
    nodes,
    edges,
    onNodesChange,
    onEdgesChange,
    onConnect,
    setSelectedNodeId,
    selectedNodeId,
    selectedEdgeId,
    deleteNode,
    deleteEdge,
    setViewportCenter,
    addNode,
    undo,
    redo,
    clearExecutionStates,
    replayJobId,
    replayTimestamp,
    exitReplayMode,
    replaySelectedNodeId,
    replayFocusNonce,
  } = useWorkflowStore();

  const isInReplayMode = replayJobId !== null;
  const lastReplayFocusNonceRef = useRef<number | null>(null);
  const lastLoadedWorkflowFitKeyRef = useRef<string | null>(null);
  const lastAggregationFrameIdsRef = useRef<Set<string>>(new Set());
  const pendingAggregationFitRef = useRef(false);

  const reactFlowInstance = useReactFlow();
  const viewportInitialized = reactFlowInstance.viewportInitialized;

  const handleNodeClick = useCallback(
    (_event: React.MouseEvent, node: { id: string; data: NodeData }) => {
      setSelectedNodeId(node.id);
    },
    [setSelectedNodeId],
  );

  const handlePaneClick = useCallback(() => {
    setSelectedNodeId(null);
  }, [setSelectedNodeId]);

  // Note: Edge selection is automatically handled by React Flow through onEdgesChange
  // when an edge is clicked. The 'select' change type is processed in the store.

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();

      const nodeType = event.dataTransfer.getData('application/reactflow');
      if (!nodeType) return;

      // Get the position where the node was dropped
      const reactFlowBounds = (event.target as HTMLElement).closest('.react-flow')?.getBoundingClientRect();

      if (!reactFlowBounds) return;

      const position = reactFlowInstance.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });

      addNode(nodeType as NodeType, position);
    },
    [reactFlowInstance, addNode],
  );

  const fitRenderedWorkflow = useCallback(() => {
    if (!viewportInitialized) return;
    if (nodes.length === 0) return;
    const bounds = document.querySelector('.react-flow')?.getBoundingClientRect();
    if (!bounds || bounds.width <= 0 || bounds.height <= 0) return;

    const nodeBounds = nodes.reduce(
      (acc, node) => {
        const size = workflowNodeLayoutSize(node);
        return {
          minX: Math.min(acc.minX, node.position.x),
          minY: Math.min(acc.minY, node.position.y),
          maxX: Math.max(acc.maxX, node.position.x + size.width),
          maxY: Math.max(acc.maxY, node.position.y + size.height),
        };
      },
      { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity },
    );
    if (!Number.isFinite(nodeBounds.minX) || !Number.isFinite(nodeBounds.minY)) return;

    const graphWidth = nodeBounds.maxX - nodeBounds.minX;
    const graphHeight = nodeBounds.maxY - nodeBounds.minY;
    const paddingX = Math.max(graphWidth * FIT_VIEW_PADDING, MIN_FIT_PADDING_PX);
    const paddingY = Math.max(graphHeight * FIT_VIEW_PADDING, MIN_FIT_PADDING_PX);
    const paddedWidth = graphWidth + paddingX * 2;
    const paddedHeight = graphHeight + paddingY * 2;
    const zoom = Math.min(bounds.width / paddedWidth, bounds.height / paddedHeight, 1);
    const paddedCenterX = nodeBounds.minX + graphWidth / 2;
    const paddedCenterY = nodeBounds.minY + graphHeight / 2;

    const viewport = {
      x: bounds.width / 2 - paddedCenterX * zoom,
      y: bounds.height / 2 - paddedCenterY * zoom,
      zoom,
    };
    void reactFlowInstance.setViewport(viewport);
  }, [nodes, reactFlowInstance, viewportInitialized]);

  // Update viewport center whenever the viewport changes
  useEffect(() => {
    const updateViewportCenter = () => {
      const viewport = reactFlowInstance.getViewport();
      const { x, y, zoom } = viewport;

      // Get the center of the visible area in flow coordinates
      // Account for the container dimensions
      const bounds = document.querySelector('.react-flow')?.getBoundingClientRect();
      if (bounds) {
        const centerX = (bounds.width / 2 - x) / zoom;
        const centerY = (bounds.height / 2 - y) / zoom;
        setViewportCenter({ x: centerX, y: centerY });
      }
    };

    // Update immediately
    updateViewportCenter();

    // Update on viewport change (pan/zoom)
    const timer = setInterval(updateViewportCenter, 100);
    return () => clearInterval(timer);
  }, [reactFlowInstance, setViewportCenter]);

  // Handle keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      // Don't handle shortcuts if user is typing in an input field
      const target = event.target as HTMLElement;
      if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') {
        return;
      }

      // Delete/Backspace - handle both nodes and edges
      if (event.key === 'Delete' || event.key === 'Backspace') {
        // Prioritize node deletion if both node and edge are selected
        if (selectedNodeId) {
          event.preventDefault();
          deleteNode(selectedNodeId);
          return;
        }
        // Delete edge if one is selected
        if (selectedEdgeId) {
          event.preventDefault();
          deleteEdge(selectedEdgeId);
          return;
        }
      }

      // Undo: Ctrl+Z (or Cmd+Z on Mac)
      if ((event.ctrlKey || event.metaKey) && event.key === 'z' && !event.shiftKey) {
        event.preventDefault();
        undo();
        return;
      }

      // Redo: Ctrl+Shift+Z or Ctrl+Y (or Cmd+Shift+Z / Cmd+Y on Mac)
      if (
        (event.ctrlKey || event.metaKey) &&
        ((event.shiftKey && event.key === 'z') || (!event.shiftKey && event.key === 'y'))
      ) {
        event.preventDefault();
        redo();
        return;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [selectedNodeId, selectedEdgeId, deleteNode, deleteEdge, undo, redo]);

  // Focus viewport when replay node selection changes.
  useEffect(() => {
    if (!replaySelectedNodeId) {
      lastReplayFocusNonceRef.current = null;
      return;
    }
    const baseNodeId = getReplayBaseNodeId(replaySelectedNodeId);
    const node = nodes.find((n) => n.id === replaySelectedNodeId) || nodes.find((n) => n.id === baseNodeId);
    if (!node) return;

    // Process each explicit replay focus request exactly once to avoid feedback loops.
    if (lastReplayFocusNonceRef.current === replayFocusNonce) {
      return;
    }
    lastReplayFocusNonceRef.current = replayFocusNonce;

    const viewport = reactFlowInstance.getViewport();
    reactFlowInstance.setCenter(node.position.x, node.position.y, {
      zoom: viewport.zoom,
      duration: 300,
    });
  }, [replaySelectedNodeId, replayFocusNonce, nodes, reactFlowInstance]);

  useEffect(() => {
    const fitKey = workflowId || sourceJobId || null;
    if (!fitKey || nodes.length === 0 || !viewportInitialized) return;
    if (lastLoadedWorkflowFitKeyRef.current === fitKey) return;

    const timer = window.setTimeout(() => {
      lastLoadedWorkflowFitKeyRef.current = fitKey;
      fitRenderedWorkflow();
    }, 100);
    return () => window.clearTimeout(timer);
  }, [workflowId, sourceJobId, nodes.length, fitRenderedWorkflow, viewportInitialized]);

  useEffect(() => {
    const frameIds = nodes
      .filter((node) => node.data.type === 'aggregation_frame')
      .map((node) => node.id)
      .sort();
    const previousFrameIds = lastAggregationFrameIdsRef.current;
    const hasNewFrame = frameIds.some((id) => !previousFrameIds.has(id));
    lastAggregationFrameIdsRef.current = new Set(frameIds);
    if (hasNewFrame) {
      pendingAggregationFitRef.current = true;
    }
  }, [nodes]);

  useEffect(() => {
    if (!pendingAggregationFitRef.current || !viewportInitialized) return;

    const timer = window.setTimeout(() => {
      pendingAggregationFitRef.current = false;
      fitRenderedWorkflow();
    }, 100);
    return () => window.clearTimeout(timer);
  }, [fitRenderedWorkflow, viewportInitialized]);

  return (
    <div className="relative h-full w-full">
      {/* Replay mode banner */}
      {isInReplayMode && (
        <Card className="bg-amber-50/95 border-amber-200 absolute inset-x-0 top-0 z-[100] flex items-center justify-between rounded-none border-x-0 border-t-0 px-5 py-3 shadow-sm">
          <div className="flex items-center gap-3">
            <span className="text-lg" role="img" aria-label="Replay mode">
              📜
            </span>
            <div>
              <div className="text-sm font-semibold text-amber-800">Viewing Execution Replay</div>
              <div className="text-muted-foreground text-xs">
                {replayTimestamp && new Date(replayTimestamp).toLocaleString()}
                {' • '}
                <span className="font-mono text-[11px]">{replayJobId?.slice(0, 8)}...</span>
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button asChild variant="outline" size="sm">
              <a href={`/admin/jobs/${replayJobId}`} target="_blank" rel="noopener noreferrer">
                View in Admin
              </a>
            </Button>
            <Button type="button" onClick={exitReplayMode} size="sm">
              Exit Replay
            </Button>
            <Badge variant="warning">Read-only</Badge>
          </div>
        </Card>
      )}
      <WorkflowToolbar
        onExecute={onExecute}
        isExecuting={isExecuting || isInReplayMode}
        workflowId={workflowId}
        workflowName={workflowName}
        workflowIsSaved={workflowIsSaved}
        sourceJobId={sourceJobId}
        onWorkflowSaved={onWorkflowSaved}
      />
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeClick={handleNodeClick}
        onPaneClick={handlePaneClick}
        onDrop={onDrop}
        onDragOver={onDragOver}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.1}
      >
        <Background variant={BackgroundVariant.Dots} gap={12} size={1} />
        <Controls />
        <MiniMap />
      </ReactFlow>
      {executionState && !isInReplayMode && (
        <ResultsOverlay executionState={executionState} onClose={clearExecutionStates} />
      )}
    </div>
  );
};

// Wrapper component that provides ReactFlow context
const WorkflowCanvas: React.FC<WorkflowCanvasProps> = (props) => {
  return (
    <ReactFlowProvider>
      <WorkflowCanvasInternal {...props} />
    </ReactFlowProvider>
  );
};

export default WorkflowCanvas;
