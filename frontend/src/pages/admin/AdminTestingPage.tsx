import { useState } from 'react';
import { adminClient } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Textarea } from '@/components/ui/textarea';
import { DEFAULT_MODEL } from '@/types/workflow';

const defaultWorkflow = {
  id: 'admin-test-workflow',
  name: 'Admin Test Workflow',
  aggregation_method: 'synthesis',
  context: {
    user_prompt: 'What is the capital of France?',
  },
  nodes: [
    {
      id: 'answer',
      type: 'prompt',
      prompt: 'Answer the question in one sentence: {{user_prompt}}',
      model: DEFAULT_MODEL,
    },
  ],
};

export default function AdminTestingPage() {
  const [payload, setPayload] = useState(JSON.stringify(defaultWorkflow, null, 2));
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState('');
  const [error, setError] = useState('');

  const submit = async () => {
    setError('');
    setResult('');
    let workflow: Record<string, unknown>;
    try {
      workflow = JSON.parse(payload) as Record<string, unknown>;
    } catch {
      setError('Invalid JSON payload');
      return;
    }

    setRunning(true);
    try {
      const response = await adminClient.testWorkflow(workflow);
      setResult(JSON.stringify(response, null, 2));
    } catch (err) {
      setError((err as Error).message || 'Failed to run test workflow');
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="space-y-4">
      <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Testing' }]} />

      <Card>
        <CardHeader>
          <CardTitle>Workflow Test Runner</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Textarea
            className="min-h-[320px] border-zinc-300 bg-zinc-50 font-mono text-xs text-zinc-900 focus:border-zinc-400 focus:ring-zinc-400"
            value={payload}
            onChange={(event) => setPayload(event.target.value)}
          />
          <Button onClick={submit} disabled={running}>
            {running ? 'Running...' : 'Run Workflow Test'}
          </Button>
          {error ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
          ) : null}
        </CardContent>
      </Card>

      {result ? (
        <Card>
          <CardHeader>
            <CardTitle>Result</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="max-h-[520px] overflow-auto rounded-md bg-zinc-950 p-4 text-xs text-zinc-100">{result}</pre>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
