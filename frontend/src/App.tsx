import { AlertTriangle, ArrowLeft, FilePlus2, PanelRight } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Route, Routes, useNavigate, useParams } from 'react-router-dom';
import { workflowClient } from './api/workflowClient';
import AdminLayout from './components/admin/AdminLayout';
import InspectorPanel from './components/panels/InspectorPanel';
import { Button } from './components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './components/ui/card';
import NodePalette from './components/workflow/NodePalette';
import WorkflowCanvas from './components/workflow/WorkflowCanvas';
import { useWorkflowExecution } from './hooks/useWorkflowExecution';
import AdminAPIPage from './pages/admin/AdminAPIPage';
import AdminBenchloopPage from './pages/admin/AdminBenchloopPage';
import AdminBenchmarkAnalysisPage from './pages/admin/AdminBenchmarkAnalysisPage';
import AdminBenchmarkComparisonPage from './pages/admin/AdminBenchmarkComparisonPage';
import AdminBenchmarkDetailPage from './pages/admin/AdminBenchmarkDetailPage';
import AdminBenchmarkItemDetailPage from './pages/admin/AdminBenchmarkItemDetailPage';
import AdminBenchmarksPage from './pages/admin/AdminBenchmarksPage';
import AdminJobDetailPage from './pages/admin/AdminJobDetailPage';
import AdminJobsPage from './pages/admin/AdminJobsPage';
import AdminOptimizeComparePage from './pages/admin/AdminOptimizeComparePage';
import AdminOptimizeDetailPage from './pages/admin/AdminOptimizeDetailPage';
import AdminOptimizePage from './pages/admin/AdminOptimizePage';
import AdminOrganismDetailPage from './pages/admin/AdminOrganismDetailPage';
import AdminOverviewPage from './pages/admin/AdminOverviewPage';
import AdminTestingPage from './pages/admin/AdminTestingPage';
import AdminWorkflowDetailPage from './pages/admin/AdminWorkflowDetailPage';
import AdminWorkflowsPage from './pages/admin/AdminWorkflowsPage';
import Ensemble from './pages/Ensemble';
import { useWorkflowStore } from './stores/workflowStore';
import type { FlowEdge, FlowNode } from './types/workflow';
import { runtimeWorkflowToWorkflowFile, workflowFileToFlow } from './utils/workflowConverter';
import { NEW_WORKFLOW_PATH, WORKFLOW_BUILDER_ROUTE_PATHS, workflowDetailPath } from './utils/workflowRoutes';

function WorkflowBuilder() {
  const [workflowId, setWorkflowId] = useState<string | undefined>();
  const [workflowName, setWorkflowName] = useState('My Workflow');
  const [workflowIsSaved, setWorkflowIsSaved] = useState(false);
  const [sourceJobId, setSourceJobId] = useState<string | undefined>();
  const [showInspector, setShowInspector] = useState(true);
  const [loadError, setLoadError] = useState<{ id: string; message: string } | null>(null);
  const { executionState, execute, clear: clearMessages } = useWorkflowExecution();
  const { loadWorkflow, clearWorkflow, updateNodeExecutionState, clearExecutionStates } = useWorkflowStore();
  const params = useParams();
  const navigate = useNavigate();
  const lastProcessedIndexRef = useRef<number>(-1);

  // Load workflow from URL parameter
  useEffect(() => {
    const id = params.id;
    const jobId = params.jobId;
    let cancelled = false;
    setWorkflowId(undefined);
    setWorkflowIsSaved(false);
    setSourceJobId(undefined);
    setLoadError(null);
    setWorkflowName(jobId ? 'Job Snapshot' : id ? 'Workflow' : 'My Workflow');
    clearWorkflow();

    if (jobId) {
      workflowClient
        .getJobWorkflow(jobId)
        .then((snapshot) => {
          if (cancelled) return;
          const workflowFile = runtimeWorkflowToWorkflowFile(snapshot.workflow, {
            jobId,
            source: snapshot.source,
            savedWorkflowExists: snapshot.saved_workflow_exists,
          });
          const { nodes, edges } = workflowFileToFlow(workflowFile);
          loadWorkflow(nodes as FlowNode[], edges as FlowEdge[]);
          setWorkflowId(workflowFile.id || snapshot.workflow_id || undefined);
          setWorkflowName(workflowFile.name || 'Ad-hoc Workflow');
          setWorkflowIsSaved(snapshot.saved_workflow_exists);
          setSourceJobId(jobId);
        })
        .catch((error) => {
          if (cancelled) return;
          console.error('Failed to load job workflow snapshot:', error);
          setLoadError({ id: jobId, message: error.message });
        });
      return () => {
        cancelled = true;
      };
    }

    if (id) {
      workflowClient
        .getWorkflow(id)
        .then((workflowFile) => {
          if (cancelled) return;
          const { nodes, edges } = workflowFileToFlow(workflowFile);
          loadWorkflow(nodes as FlowNode[], edges as FlowEdge[]);
          setWorkflowId(id);
          setWorkflowName(workflowFile.name || 'My Workflow');
          setWorkflowIsSaved(true);
          setSourceJobId(undefined);
        })
        .catch((error) => {
          if (cancelled) return;
          console.error('Failed to load workflow:', error);
          setLoadError({ id, message: error.message });
        });
      return () => {
        cancelled = true;
      };
    }
    return () => {
      cancelled = true;
    };
  }, [params.id, params.jobId, loadWorkflow, clearWorkflow]);

  // Clear execution states when execution starts
  useEffect(() => {
    if (executionState.isExecuting && executionState.messages.length === 0) {
      // Clear states immediately when execution starts
      clearExecutionStates();
      lastProcessedIndexRef.current = -1;
    }
  }, [executionState.isExecuting, executionState.messages.length, clearExecutionStates]);

  // Sync execution messages with node execution states
  useEffect(() => {
    // Only process new messages (not already processed)
    const newMessages = executionState.messages.slice(lastProcessedIndexRef.current + 1);

    newMessages.forEach((msg) => {
      if (msg.node_id) {
        if (msg.type === 'node_start') {
          updateNodeExecutionState(msg.node_id, {
            status: 'running',
            startTime: msg.timestamp,
          });
        } else if (msg.type === 'node_complete') {
          updateNodeExecutionState(msg.node_id, {
            status: 'completed',
            endTime: msg.timestamp,
            output: msg.output,
            metrics: msg.data
              ? {
                  tokens_input: msg.data.tokens_input,
                  tokens_output: msg.data.tokens_output,
                  cost: msg.data.cost,
                  latency_ms: msg.data.latency_ms,
                }
              : undefined,
          });
        } else if (msg.type === 'error' || msg.type === 'node_failed') {
          updateNodeExecutionState(msg.node_id, {
            status: 'error',
            error: msg.error || msg.message,
          });
        }
      }
    });

    // Update the last processed index
    if (executionState.messages.length > 0) {
      lastProcessedIndexRef.current = executionState.messages.length - 1;
    }
  }, [executionState.messages, updateNodeExecutionState]);

  // Show error state if workflow couldn't be loaded
  if (loadError) {
    return (
      <div className="from-background to-muted/25 flex h-screen items-center justify-center bg-gradient-to-br p-8">
        <Card className="w-full max-w-xl border-amber-200/70 shadow-xl">
          <CardHeader className="items-center text-center">
            <div className="bg-amber-100 text-amber-700 mb-2 inline-flex h-12 w-12 items-center justify-center rounded-full">
              <AlertTriangle className="h-6 w-6" />
            </div>
            <CardTitle>Workflow Not Found</CardTitle>
            <CardDescription>
              The workflow <code className="bg-muted rounded px-1.5 py-0.5 font-mono">{loadError.id}</code> could not be
              loaded.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4 text-center text-sm">
            <p className="text-muted-foreground">
              This may be a seed workflow that can only be run from the Ensemble page, or the workflow may have been
              deleted.
            </p>
            <div className="flex flex-wrap justify-center gap-2">
              <Button type="button" onClick={() => navigate('/ensemble')} variant="secondary">
                Go to Ensemble
              </Button>
              <Button type="button" onClick={() => navigate(NEW_WORKFLOW_PATH)}>
                <FilePlus2 className="h-4 w-4" />
                New Workflow
              </Button>
              <Button type="button" onClick={() => window.history.back()} variant="outline">
                <ArrowLeft className="h-4 w-4" />
                Go Back
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="bg-background flex h-screen overflow-hidden">
      <NodePalette />
      <div className="relative flex-1">
        <WorkflowCanvas
          onExecute={(workflow) => {
            execute(workflow);
          }}
          isExecuting={executionState.isExecuting}
          workflowId={workflowId}
          workflowName={workflowName}
          workflowIsSaved={workflowIsSaved}
          sourceJobId={sourceJobId}
          onWorkflowSaved={(id, name) => {
            setWorkflowId(id);
            setWorkflowName(name);
            setWorkflowIsSaved(true);
            setSourceJobId(undefined);
            navigate(workflowDetailPath(id));
          }}
          executionState={executionState}
        />
      </div>
      {showInspector && (
        <InspectorPanel executionState={executionState} onClear={clearMessages} workflowId={workflowId} />
      )}
      {/* Toggle button for inspector panel */}
      <Button
        type="button"
        size="sm"
        onClick={() => setShowInspector(!showInspector)}
        className="absolute top-3 z-[1000] gap-1.5 shadow"
        style={{ right: showInspector ? '390px' : '12px' }}
        title={showInspector ? 'Hide inspector' : 'Show inspector'}
      >
        <PanelRight className="h-3.5 w-3.5" />
        {showInspector ? 'Hide' : 'Inspector'}
      </Button>
    </div>
  );
}

function App() {
  return (
    <Routes>
      <Route path="/" element={<Ensemble />} />
      <Route path="/ensemble" element={<Ensemble />} />
      {WORKFLOW_BUILDER_ROUTE_PATHS.map((path) => (
        <Route key={path} path={path} element={<WorkflowBuilder />} />
      ))}
      <Route path="/workflow/:id" element={<WorkflowBuilder />} />
      <Route path="/workflow/from-job/:jobId" element={<WorkflowBuilder />} />
      <Route path="/admin" element={<AdminLayout />}>
        <Route index element={<AdminOverviewPage />} />
        <Route path="api" element={<AdminAPIPage />} />
        <Route path="jobs" element={<AdminJobsPage />} />
        <Route path="jobs/:id" element={<AdminJobDetailPage />} />
        <Route path="workflows" element={<AdminWorkflowsPage />} />
        <Route path="workflows/:id" element={<AdminWorkflowDetailPage />} />
        <Route path="benchmarks" element={<AdminBenchmarksPage />} />
        <Route path="benchmarks/compare" element={<AdminBenchmarkComparisonPage />} />
        <Route path="benchmarks/:id" element={<AdminBenchmarkDetailPage />} />
        <Route path="benchmarks/:id/analysis" element={<AdminBenchmarkAnalysisPage />} />
        <Route path="benchmarks/:id/items/*" element={<AdminBenchmarkItemDetailPage />} />
        <Route path="optimize" element={<AdminOptimizePage />} />
        <Route path="optimize/compare" element={<AdminOptimizeComparePage />} />
        <Route path="optimize/:id" element={<AdminOptimizeDetailPage />} />
        <Route path="optimize/:id/organisms/:orgId" element={<AdminOrganismDetailPage />} />
        <Route path="benchloop" element={<AdminBenchloopPage />} />
        <Route path="testing" element={<AdminTestingPage />} />
      </Route>
    </Routes>
  );
}

export default App;
