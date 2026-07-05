import React, { useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import { workflowClient } from '../../api/workflowClient';
import type { ExecuteWorkflowRequest } from '../../hooks/useWorkflowExecution';
import { useWorkflowStore } from '../../stores/workflowStore';
import type { WorkflowFileFormat } from '../../types/workflowFile';
import { extractInputSchema, flowToWorkflowFile, workflowFileToFlow } from '../../utils/workflowConverter';
import { NEW_WORKFLOW_PATH } from '../../utils/workflowRoutes';
import { InputDialog } from '../dialogs/InputDialog';

interface SchemaField {
  type?: string;
  label?: string;
  description?: string;
  required?: boolean;
  placeholder?: string;
}

interface WorkflowToolbarProps {
  onExecute?: (request: ExecuteWorkflowRequest) => void;
  isExecuting?: boolean;
  workflowId?: string;
  workflowName?: string;
  workflowIsSaved?: boolean;
  sourceJobId?: string;
  onWorkflowSaved?: (id: string, name: string) => void;
}

const WorkflowToolbar: React.FC<WorkflowToolbarProps> = ({
  onExecute,
  isExecuting = false,
  workflowId: initialWorkflowId,
  workflowName: initialWorkflowName,
  workflowIsSaved: initialWorkflowIsSaved = false,
  sourceJobId,
  onWorkflowSaved,
}) => {
  const { nodes, edges, loadWorkflow, clearWorkflow } = useWorkflowStore();
  const navigate = useNavigate();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [workflowName, setWorkflowName] = useState(initialWorkflowName || 'My Workflow');
  const [workflowId, setWorkflowId] = useState(initialWorkflowId);
  const [workflowIsSaved, setWorkflowIsSaved] = useState(initialWorkflowIsSaved);
  const [showInputDialog, setShowInputDialog] = useState(false);
  const [inputSchema, setInputSchema] = useState<Record<string, SchemaField>>({});
  const [isEditingName, setIsEditingName] = useState(false);
  const [editedName, setEditedName] = useState(workflowName);

  // Update local state when props change
  React.useEffect(() => {
    if (initialWorkflowName) {
      setWorkflowName(initialWorkflowName);
      setEditedName(initialWorkflowName);
    }
  }, [initialWorkflowName]);

  React.useEffect(() => {
    setWorkflowId(initialWorkflowId);
  }, [initialWorkflowId]);

  React.useEffect(() => {
    setWorkflowIsSaved(initialWorkflowIsSaved);
  }, [initialWorkflowIsSaved]);

  const handleExport = () => {
    try {
      console.log('Exporting workflow...', { nodes, edges, workflowName });
      const workflowFile = flowToWorkflowFile(nodes, edges, workflowName);
      console.log('Converted workflow file:', workflowFile);
      const json = JSON.stringify(workflowFile, null, 2);
      const blob = new Blob([json], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `${workflowName.replace(/\s+/g, '-').toLowerCase()}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
      console.log('Export successful');
    } catch (error) {
      console.error('Export error:', error);
      alert(`Export failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const handleImport = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = (e) => {
      try {
        const json = e.target?.result as string;
        const workflowFile = JSON.parse(json) as WorkflowFileFormat;

        if (workflowFile.nodes && workflowFile.edges) {
          const { nodes: newNodes, edges: newEdges } = workflowFileToFlow(workflowFile);
          loadWorkflow(newNodes, newEdges);
          setWorkflowName(workflowFile.name || 'Imported Workflow');
          setWorkflowId(undefined); // Clear ID for imported workflows (they should be saved as new)
          setWorkflowIsSaved(false);
          navigate(NEW_WORKFLOW_PATH);
        } else {
          alert('Invalid workflow file format');
        }
      } catch (error) {
        console.error('Import error:', error);
        alert(`Import failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
      }
    };
    reader.readAsText(file);

    // Reset input so the same file can be imported again
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  const handleNew = () => {
    if (nodes.length > 0) {
      if (!confirm('Clear current workflow?')) return;
    }
    clearWorkflow();
    setWorkflowName('My Workflow');
    setWorkflowId(undefined);
    setWorkflowIsSaved(false);
    navigate(NEW_WORKFLOW_PATH);
  };

  const handleRename = async () => {
    const trimmedName = editedName.trim();
    if (!trimmedName) {
      alert('Workflow name cannot be empty');
      setEditedName(workflowName); // Reset to previous name
      setIsEditingName(false);
      return;
    }

    if (trimmedName === workflowName) {
      setIsEditingName(false);
      return;
    }

    // Update local state immediately
    setWorkflowName(trimmedName);
    setIsEditingName(false);

    // If workflow is saved, update on backend
    if (workflowId && workflowIsSaved) {
      try {
        const workflowFile = flowToWorkflowFile(nodes, edges, trimmedName, '', workflowId);

        await workflowClient.updateWorkflow(workflowId, workflowFile);

        // Notify parent component
        if (onWorkflowSaved) {
          onWorkflowSaved(workflowId, trimmedName);
        }
      } catch (error) {
        alert(`Rename failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
        // Revert to old name on error
        setWorkflowName(workflowName);
        setEditedName(workflowName);
      }
    }
  };

  const handleSave = async () => {
    if (nodes.length === 0) {
      alert('No workflow to save');
      return;
    }

    try {
      // Create workflow file format (same as export)
      // Note: IDs may be temporary (temp-xxx), backend will normalize them
      const workflowFile = flowToWorkflowFile(nodes, edges, workflowName, '', workflowId);

      if (!workflowIsSaved && workflowId) {
        try {
          await workflowClient.getWorkflow(workflowId);
          const shouldOverwrite = confirm(
            `A saved workflow with ID "${workflowId}" already exists. Overwrite it? Cancel will save this snapshot as a new workflow.`,
          );
          if (!shouldOverwrite) {
            workflowFile.id = undefined;
          }
        } catch (error) {
          const status = (error as { response?: { status?: number } }).response?.status;
          if (status !== 404) {
            throw error;
          }
        }
      }

      const result = await workflowClient.saveWorkflow(workflowFile);

      // Backend returns normalized workflow with all IDs assigned
      const savedWorkflow = result.workflow;
      const savedId = result.id || savedWorkflow.id;

      // Update local state with backend-assigned IDs
      setWorkflowId(savedId);
      setWorkflowIsSaved(true);

      // Sync frontend state with backend-assigned IDs
      if (savedWorkflow?.nodes && savedWorkflow.edges) {
        loadWorkflow(savedWorkflow.nodes, savedWorkflow.edges);
      }

      // Notify parent component
      if (onWorkflowSaved) {
        onWorkflowSaved(savedId, workflowName);
      }

      alert(`Workflow saved successfully!`);
    } catch (error) {
      alert(`Save failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const handleExecute = () => {
    if (nodes.length === 0) {
      alert('No workflow to execute');
      return;
    }

    if (!onExecute) {
      alert('Execution handler not configured');
      return;
    }

    try {
      // Extract input schema from input nodes
      const schema = extractInputSchema(nodes) as Record<string, SchemaField>;

      // If there are inputs required, show dialog
      if (Object.keys(schema).length > 0) {
        setInputSchema(schema);
        setShowInputDialog(true);
      } else {
        // No inputs required, execute immediately
        executeWithInputs({});
      }
    } catch (error) {
      alert(`Failed to prepare workflow: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  const executeWithInputs = (inputValues: Record<string, unknown>) => {
    try {
      // Backend owns conversion from editor graph to executable workflow.
      const workflowFile = flowToWorkflowFile(nodes, edges, workflowName, '', workflowId);
      console.log('Executing workflow via WebSocket:', workflowFile);
      onExecute!({ workflowFile, inputValues });
      setShowInputDialog(false);
    } catch (error) {
      alert(`Failed to prepare workflow file: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }
  };

  return (
    <>
      <Card className="bg-card/95 border-border/80 absolute top-3 left-1/2 z-10 flex max-w-[calc(100%-2rem)] -translate-x-1/2 flex-row flex-nowrap items-center justify-center gap-2 overflow-x-auto px-4 py-2 shadow-lg backdrop-blur-sm">
        {isEditingName ? (
          <Input
            type="text"
            value={editedName}
            onChange={(e) => setEditedName(e.target.value)}
            onBlur={handleRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                handleRename();
              } else if (e.key === 'Escape') {
                setEditedName(workflowName);
                setIsEditingName(false);
              }
            }}
            className="h-8 min-w-[200px] max-w-[280px] font-semibold"
            autoFocus
          />
        ) : (
          <span
            role="button"
            tabIndex={0}
            onClick={() => {
              setIsEditingName(true);
              setEditedName(workflowName);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                setIsEditingName(true);
                setEditedName(workflowName);
              }
            }}
            className="hover:bg-accent hover:text-accent-foreground max-w-[220px] cursor-pointer truncate whitespace-nowrap rounded-md px-2 py-1 text-sm font-semibold transition-colors"
            title="Click to rename"
          >
            {workflowName}
          </span>
        )}

        <Button type="button" variant="outline" size="sm" onClick={handleNew}>
          New
        </Button>

        <Button type="button" variant="secondary" size="sm" onClick={() => fileInputRef.current?.click()}>
          Import
        </Button>

        <Button type="button" size="sm" onClick={handleSave} disabled={nodes.length === 0}>
          {workflowIsSaved ? 'Save' : 'Save Snapshot'}
        </Button>

        <Button
          type="button"
          onClick={() => {
            if (nodes.length > 0) {
              handleExport();
            }
          }}
          variant="default"
          size="sm"
          disabled={nodes.length === 0}
        >
          Export
        </Button>

        <Separator orientation="vertical" className="mx-1 h-6" />

        <Button type="button" onClick={handleExecute} size="sm" disabled={nodes.length === 0 || isExecuting}>
          {isExecuting ? 'Executing...' : 'Execute'}
        </Button>

        {workflowId && workflowIsSaved && (
          <Button type="button" variant="outline" size="sm" asChild>
            <a href={`/admin/workflows/${workflowId}`} target="_blank" rel="noopener noreferrer">
              Admin
            </a>
          </Button>
        )}

        {!workflowIsSaved && sourceJobId && (
          <Badge variant="secondary" className="ml-2 max-w-[180px] truncate" title={`Job snapshot ${sourceJobId}`}>
            Job Snapshot
          </Badge>
        )}

        <Badge variant="outline" className="ml-2">
          {nodes.length} nodes
        </Badge>
      </Card>

      <input
        ref={fileInputRef}
        type="file"
        accept="application/json"
        style={{ display: 'none' }}
        onChange={handleImport}
      />

      <InputDialog
        isOpen={showInputDialog}
        schema={inputSchema}
        onSubmit={executeWithInputs}
        onCancel={() => setShowInputDialog(false)}
      />
    </>
  );
};

export default WorkflowToolbar;
