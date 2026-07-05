import type React from 'react';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { cn } from '@/lib/utils';
import { useWorkflowStore } from '../../stores/workflowStore';
import type { NodeType } from '../../types/workflow';

const nodeTypes: { type: NodeType; label: string; color: string }[] = [
  { type: 'input', label: 'Input', color: 'bg-emerald-500 hover:bg-emerald-400' },
  { type: 'agent', label: 'LLM Prompt', color: 'bg-sky-500 hover:bg-sky-400' },
  { type: 'agent_run', label: 'Novomo Agent', color: 'bg-teal-600 hover:bg-teal-500' },
  { type: 'novo_run', label: 'Superagent', color: 'bg-rose-600 hover:bg-rose-500' },
  { type: 'workflow_ref', label: 'Use Workflow', color: 'bg-cyan-600 hover:bg-cyan-500' },
  { type: 'child_workflow', label: 'Child Workflow', color: 'bg-violet-500 hover:bg-violet-400' },
  { type: 'conditional', label: 'Conditional', color: 'bg-amber-500 hover:bg-amber-400' },
  { type: 'aggregation', label: 'Aggregation', color: 'bg-teal-500 hover:bg-teal-400' },
  { type: 'operation', label: 'Operation', color: 'bg-slate-600 hover:bg-slate-500' },
  { type: 'result', label: 'Result', color: 'bg-indigo-500 hover:bg-indigo-400' },
];

const NodePalette: React.FC = () => {
  const addNode = useWorkflowStore((state) => state.addNode);
  const selectedNodeId = useWorkflowStore((state) => state.selectedNodeId);
  const selectedNode = useWorkflowStore((state) => state.nodes.find((node) => node.id === selectedNodeId));
  const editingForkedAggregation = selectedNode?.data.config.aggregationInternalState === 'forked';

  const handleAddNode = (type: NodeType) => {
    // Add node to center of current viewport (store tracks this automatically)
    addNode(type);
  };

  const onDragStart = (event: React.DragEvent, nodeType: NodeType) => {
    event.dataTransfer.setData('application/reactflow', nodeType);
    event.dataTransfer.effectAllowed = 'move';
  };

  return (
    <Card className="bg-card/95 border-border/70 flex w-[220px] shrink-0 flex-col gap-3 rounded-none border-y-0 border-l-0 p-4">
      <h3 className="text-sm font-semibold tracking-wide">Node Palette</h3>
      {nodeTypes
        .filter(({ type }) => type !== 'operation' || editingForkedAggregation)
        .map(({ type, label, color }) => (
          <Button
            type="button"
            key={type}
            onClick={() => handleAddNode(type)}
            onDragStart={(e) => onDragStart(e, type)}
            draggable
            className={cn(
              'h-10 cursor-grab justify-center border-0 font-medium text-white active:cursor-grabbing',
              color,
            )}
          >
            + {label}
          </Button>
        ))}
      <div className="mt-4 border-t pt-4">
        <Button
          type="button"
          onClick={() => useWorkflowStore.getState().clearWorkflow()}
          variant="destructive"
          size="sm"
          className="w-full"
        >
          Clear All
        </Button>
      </div>
    </Card>
  );
};

export default NodePalette;
