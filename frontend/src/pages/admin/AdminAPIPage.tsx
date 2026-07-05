import { Clipboard, Download, KeyRound, RefreshCcw, Route, Save, ShieldAlert, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  type AdminAPIKey,
  type AdminAPIModelRoute,
  type AdminAPIUsageFilters,
  type AdminAPIUsageRecord,
  type AdminAPIUsageSummary,
  adminClient,
} from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { NativeSelect } from '@/components/ui/native-select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { copyToClipboard, formatCost, formatTokens, relativeTime, truncateId } from '@/lib/formatters';

type RouteMode = 'workflow' | 'direct_model';

const defaultUsageSummary: AdminAPIUsageSummary = {
  requests: 0,
  tokens_input: 0,
  tokens_output: 0,
  tokens_total: 0,
  cost: 0,
};

export default function AdminAPIPage() {
  const [keys, setKeys] = useState<AdminAPIKey[]>([]);
  const [usage, setUsage] = useState<AdminAPIUsageRecord[]>([]);
  const [summary, setSummary] = useState<AdminAPIUsageSummary>(defaultUsageSummary);
  const [routes, setRoutes] = useState<AdminAPIModelRoute[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [createdSecret, setCreatedSecret] = useState('');
  const [keyForm, setKeyForm] = useState({
    name: '',
    workflow_id: '',
    requests_per_minute: 60,
    tokens_per_minute: 120000,
  });
  const [usageFilters, setUsageFilters] = useState<AdminAPIUsageFilters>({ limit: 50 });
  const [routeForm, setRouteForm] = useState({
    api_model: '',
    mode: 'direct_model' as RouteMode,
    provider_model: '',
    workflow_id: '',
    description: '',
    is_default: false,
    enabled: true,
  });

  const loadData = useCallback(async () => {
    setError('');
    setLoading(true);
    try {
      const [keyData, usageData, routeData] = await Promise.all([
        adminClient.listAPIKeys({ include_revoked: true }),
        adminClient.listAPIUsage(usageFilters),
        adminClient.listAPIModelRoutes({ include_disabled: true }),
      ]);
      setKeys(keyData.api_keys ?? []);
      setUsage(usageData.api_usage ?? usageData.usage ?? []);
      setSummary(usageData.summary ?? defaultUsageSummary);
      setRoutes(routeData.model_routes ?? []);
    } catch (err) {
      setError((err as Error).message || 'Failed to load API data');
    } finally {
      setLoading(false);
    }
  }, [usageFilters]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const activeKeys = useMemo(() => keys.filter((key) => !key.revoked_at).length, [keys]);

  const createKey = async () => {
    if (!keyForm.name.trim()) {
      setError('Key name is required');
      return;
    }
    setSaving(true);
    setError('');
    setMessage('');
    try {
      const response = await adminClient.createAPIKey({
        name: keyForm.name.trim(),
        workflow_id: keyForm.workflow_id.trim() || undefined,
        requests_per_minute: Number(keyForm.requests_per_minute) || 60,
        tokens_per_minute: Number(keyForm.tokens_per_minute) || 120000,
      });
      setCreatedSecret(response.key);
      setKeyForm({ name: '', workflow_id: '', requests_per_minute: 60, tokens_per_minute: 120000 });
      setMessage(`Created ${response.api_key.prefix}`);
      await loadData();
    } catch (err) {
      setError((err as Error).message || 'Failed to create API key');
    } finally {
      setSaving(false);
    }
  };

  const revokeKey = async (key: AdminAPIKey) => {
    setSaving(true);
    setError('');
    setMessage('');
    try {
      await adminClient.revokeAPIKey(key.id);
      setMessage(`Revoked ${key.prefix}`);
      await loadData();
    } catch (err) {
      setError((err as Error).message || 'Failed to revoke API key');
    } finally {
      setSaving(false);
    }
  };

  const refreshUsage = async () => {
    setError('');
    try {
      const data = await adminClient.listAPIUsage(usageFilters);
      setUsage(data.api_usage ?? data.usage ?? []);
      setSummary(data.summary ?? defaultUsageSummary);
    } catch (err) {
      setError((err as Error).message || 'Failed to load usage');
    }
  };

  const exportUsage = async () => {
    setError('');
    try {
      const blob = await adminClient.exportAPIUsage(usageFilters);
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'api-usage.csv';
      link.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError((err as Error).message || 'Failed to export usage');
    }
  };

  const saveRoute = async () => {
    if (!routeForm.api_model.trim()) {
      setError('API model is required');
      return;
    }
    setSaving(true);
    setError('');
    setMessage('');
    try {
      const payload = {
        api_model: routeForm.api_model.trim(),
        mode: routeForm.mode,
        workflow_id: routeForm.workflow_id.trim() || undefined,
        provider_model: routeForm.provider_model.trim() || undefined,
        description: routeForm.description.trim() || undefined,
        is_default: routeForm.is_default,
        enabled: routeForm.enabled,
      };
      const response = await adminClient.upsertAPIModelRoute(payload);
      setMessage(`Saved route ${response.model_route.api_model}`);
      setRouteForm({
        api_model: '',
        mode: 'direct_model',
        provider_model: '',
        workflow_id: '',
        description: '',
        is_default: false,
        enabled: true,
      });
      await loadData();
    } catch (err) {
      setError((err as Error).message || 'Failed to save route');
    } finally {
      setSaving(false);
    }
  };

  const deleteRoute = async (route: AdminAPIModelRoute) => {
    setSaving(true);
    setError('');
    setMessage('');
    try {
      await adminClient.deleteAPIModelRoute(route.api_model);
      setMessage(`Deleted route ${route.api_model}`);
      await loadData();
    } catch (err) {
      setError((err as Error).message || 'Failed to delete route');
    } finally {
      setSaving(false);
    }
  };

  const editRoute = (route: AdminAPIModelRoute) => {
    setRouteForm({
      api_model: route.api_model,
      mode: route.mode === 'workflow' ? 'workflow' : 'direct_model',
      provider_model: route.provider_model ?? '',
      workflow_id: route.workflow_id ?? '',
      description: route.description ?? '',
      is_default: route.is_default,
      enabled: route.enabled,
    });
  };

  if (loading && keys.length === 0 && usage.length === 0 && routes.length === 0) {
    return <EmptyState message="Loading API management..." />;
  }

  return (
    <div className="space-y-5">
      <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'API' }]} />

      <div className="flex items-start gap-3 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
        <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          Admin API controls are unauthenticated; expose this server only on localhost or behind trusted auth.
        </span>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Metric label="Active Keys" value={activeKeys.toLocaleString()} />
        <Metric label="Routes" value={routes.length.toLocaleString()} />
        <Metric label="Requests" value={summary.requests.toLocaleString()} />
        <Metric label="Cost" value={formatCost(summary.cost)} />
      </div>

      {error ? (
        <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
      ) : null}
      {message ? (
        <div className="rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">
          {message}
        </div>
      ) : null}

      <Tabs defaultValue="keys" className="space-y-4">
        <TabsList className="h-auto flex-wrap justify-start">
          <TabsTrigger value="keys">Keys</TabsTrigger>
          <TabsTrigger value="usage">Usage</TabsTrigger>
          <TabsTrigger value="routes">Routes</TabsTrigger>
          <TabsTrigger value="docs">Docs</TabsTrigger>
        </TabsList>

        <TabsContent value="keys" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <KeyRound className="h-4 w-4" />
                Create Key
              </CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3 lg:grid-cols-[minmax(180px,1.2fr)_minmax(160px,1fr)_140px_150px_auto]">
              <Input
                placeholder="Name"
                value={keyForm.name}
                onChange={(event) => setKeyForm((current) => ({ ...current, name: event.target.value }))}
              />
              <Input
                placeholder="Workflow override"
                value={keyForm.workflow_id}
                onChange={(event) => setKeyForm((current) => ({ ...current, workflow_id: event.target.value }))}
              />
              <Input
                type="number"
                min={1}
                value={keyForm.requests_per_minute}
                onChange={(event) =>
                  setKeyForm((current) => ({ ...current, requests_per_minute: Number(event.target.value) }))
                }
              />
              <Input
                type="number"
                min={1}
                value={keyForm.tokens_per_minute}
                onChange={(event) =>
                  setKeyForm((current) => ({ ...current, tokens_per_minute: Number(event.target.value) }))
                }
              />
              <Button type="button" onClick={createKey} disabled={saving}>
                <KeyRound className="h-4 w-4" />
                Create
              </Button>
            </CardContent>
          </Card>

          {createdSecret ? (
            <div className="rounded-md border border-amber-200 bg-amber-50 p-3">
              <div className="mb-2 flex items-center justify-between gap-3">
                <span className="text-sm font-medium text-amber-900">One-time key</span>
                <Button type="button" variant="outline" size="sm" onClick={() => void copyToClipboard(createdSecret)}>
                  <Clipboard className="h-4 w-4" />
                  Copy
                </Button>
              </div>
              <code className="block overflow-x-auto rounded bg-white px-3 py-2 font-mono text-xs text-amber-950">
                {createdSecret}
              </code>
            </div>
          ) : null}

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Keys</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Prefix</TableHead>
                    <TableHead>Limits</TableHead>
                    <TableHead>Workflow</TableHead>
                    <TableHead>Last Used</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {keys.map((key) => (
                    <TableRow key={key.id}>
                      <TableCell>
                        <div className="font-medium">{key.name}</div>
                        <div className="text-muted-foreground font-mono text-xs">{truncateId(key.id, 12)}</div>
                      </TableCell>
                      <TableCell className="font-mono text-xs">{key.prefix}</TableCell>
                      <TableCell className="text-sm">
                        {key.requests_per_minute}/min · {formatTokens(key.tokens_per_minute)} tok/min
                      </TableCell>
                      <TableCell className="max-w-[260px] truncate font-mono text-xs">
                        {key.workflow_id || '-'}
                      </TableCell>
                      <TableCell>{key.last_used_at ? relativeTime(key.last_used_at) : '-'}</TableCell>
                      <TableCell>
                        <Badge variant={key.revoked_at ? 'outline' : 'success'}>
                          {key.revoked_at ? 'revoked' : 'active'}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={saving || Boolean(key.revoked_at)}
                          onClick={() => void revokeKey(key)}
                        >
                          <Trash2 className="h-4 w-4" />
                          Revoke
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="usage" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Usage Filters</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3 md:grid-cols-2 xl:grid-cols-[1fr_1fr_1fr_120px_auto_auto]">
              <Input
                placeholder="Key ID"
                value={usageFilters.key_id ?? ''}
                onChange={(event) => setUsageFilters((current) => ({ ...current, key_id: event.target.value }))}
              />
              <Input
                placeholder="Model"
                value={usageFilters.model ?? ''}
                onChange={(event) => setUsageFilters((current) => ({ ...current, model: event.target.value }))}
              />
              <NativeSelect
                value={usageFilters.status ?? ''}
                onChange={(event) => setUsageFilters((current) => ({ ...current, status: event.target.value }))}
              >
                <option value="">Any status</option>
                <option value="running">running</option>
                <option value="succeeded">succeeded</option>
                <option value="failed">failed</option>
              </NativeSelect>
              <Input
                type="number"
                min={1}
                value={usageFilters.limit ?? 50}
                onChange={(event) => setUsageFilters((current) => ({ ...current, limit: Number(event.target.value) }))}
              />
              <Button type="button" variant="outline" onClick={() => void refreshUsage()}>
                <RefreshCcw className="h-4 w-4" />
                Refresh
              </Button>
              <Button type="button" onClick={() => void exportUsage()}>
                <Download className="h-4 w-4" />
                Export
              </Button>
            </CardContent>
          </Card>

          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <Metric label="Input Tokens" value={formatTokens(summary.tokens_input)} />
            <Metric label="Output Tokens" value={formatTokens(summary.tokens_output)} />
            <Metric label="Total Tokens" value={formatTokens(summary.tokens_total)} />
            <Metric label="Spend" value={formatCost(summary.cost)} />
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Usage</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Time</TableHead>
                    <TableHead>Key</TableHead>
                    <TableHead>Endpoint</TableHead>
                    <TableHead>Model</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Tokens</TableHead>
                    <TableHead>Cost</TableHead>
                    <TableHead>Error</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {usage.map((row) => (
                    <TableRow key={row.id}>
                      <TableCell title={new Date(row.created_at).toLocaleString()}>
                        {relativeTime(row.created_at)}
                      </TableCell>
                      <TableCell className="font-mono text-xs">{truncateId(row.key_id, 12)}</TableCell>
                      <TableCell className="font-mono text-xs">{row.endpoint}</TableCell>
                      <TableCell>{row.requested_model || row.resolved_model || '-'}</TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            row.status === 'succeeded' ? 'success' : row.status === 'failed' ? 'destructive' : 'info'
                          }
                        >
                          {row.status}
                        </Badge>
                      </TableCell>
                      <TableCell>{formatTokens(row.tokens_total)}</TableCell>
                      <TableCell>{formatCost(row.cost)}</TableCell>
                      <TableCell className="max-w-[280px] truncate text-xs">{row.error_message || '-'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="routes" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Route className="h-4 w-4" />
                Model Route
              </CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3 xl:grid-cols-[1fr_170px_1fr_1fr_auto_auto_auto]">
              <Input
                placeholder="API model"
                value={routeForm.api_model}
                onChange={(event) => setRouteForm((current) => ({ ...current, api_model: event.target.value }))}
              />
              <NativeSelect
                value={routeForm.mode}
                onChange={(event) => setRouteForm((current) => ({ ...current, mode: event.target.value as RouteMode }))}
              >
                <option value="direct_model">direct_model</option>
                <option value="workflow">workflow</option>
              </NativeSelect>
              <Input
                placeholder="Provider model"
                value={routeForm.provider_model}
                disabled={routeForm.mode === 'workflow'}
                onChange={(event) => setRouteForm((current) => ({ ...current, provider_model: event.target.value }))}
              />
              <Input
                placeholder="Workflow ID"
                value={routeForm.workflow_id}
                disabled={routeForm.mode === 'direct_model'}
                onChange={(event) => setRouteForm((current) => ({ ...current, workflow_id: event.target.value }))}
              />
              <div className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={routeForm.is_default}
                  onCheckedChange={(checked) =>
                    setRouteForm((current) => ({ ...current, is_default: checked === true }))
                  }
                />
                Default
              </div>
              <div className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={routeForm.enabled}
                  onCheckedChange={(checked) => setRouteForm((current) => ({ ...current, enabled: checked === true }))}
                />
                Enabled
              </div>
              <Button type="button" onClick={saveRoute} disabled={saving}>
                <Save className="h-4 w-4" />
                Save
              </Button>
              <Input
                className="xl:col-span-7"
                placeholder="Description"
                value={routeForm.description}
                onChange={(event) => setRouteForm((current) => ({ ...current, description: event.target.value }))}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Routes</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Model</TableHead>
                    <TableHead>Mode</TableHead>
                    <TableHead>Target</TableHead>
                    <TableHead>Default</TableHead>
                    <TableHead>Enabled</TableHead>
                    <TableHead>Description</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {routes.map((route) => (
                    <TableRow key={route.api_model}>
                      <TableCell className="font-mono text-xs">{route.api_model}</TableCell>
                      <TableCell>{route.mode}</TableCell>
                      <TableCell className="max-w-[280px] truncate font-mono text-xs">
                        {route.mode === 'workflow' ? route.workflow_id : route.provider_model}
                      </TableCell>
                      <TableCell>{route.is_default ? <Badge variant="info">default</Badge> : '-'}</TableCell>
                      <TableCell>
                        <Badge variant={route.enabled ? 'success' : 'outline'}>
                          {route.enabled ? 'enabled' : 'off'}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-[320px] truncate">{route.description || '-'}</TableCell>
                      <TableCell className="space-x-2 text-right">
                        <Button type="button" variant="outline" size="sm" onClick={() => editRoute(route)}>
                          Edit
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={saving}
                          onClick={() => void deleteRoute(route)}
                        >
                          <Trash2 className="h-4 w-4" />
                          Delete
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="docs" className="space-y-4">
          <Snippet
            title="Models"
            code={`curl http://localhost:8080/v1/models \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY"

curl http://localhost:8080/v1/models/consortium-default \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY"`}
          />
          <Snippet
            title="Chat Completions"
            code={`curl http://localhost:8080/v1/chat/completions \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"consortium-default","store":true,"messages":[{"role":"user","content":"Explain durable workflows."}]}'

curl http://localhost:8080/v1/chat/completions/$COMPLETION_ID/messages \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY"`}
          />
          <Snippet
            title="Responses"
            code={`curl http://localhost:8080/v1/responses \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"consortium-default","input":"Summarize the benchmark result.","text":{"format":{"type":"json_object"}}}'

curl http://localhost:8080/v1/responses/$RESPONSE_ID \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY"

curl http://localhost:8080/v1/responses/$RESPONSE_ID/input_items \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY"`}
          />
          <Snippet
            title="Background Responses"
            code={`curl http://localhost:8080/v1/responses \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY" \\
  -H "Content-Type: application/json" \\
  -H "Idempotency-Key: demo-background-1" \\
  -d '{"model":"consortium-default","input":"Run this asynchronously.","background":true}'

curl http://localhost:8080/v1/responses/$RESPONSE_ID \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY"

curl -X POST http://localhost:8080/v1/responses/$RESPONSE_ID/cancel \\
  -H "Authorization: Bearer $CONSORTIUM_API_KEY"`}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-card px-4 py-3 shadow-xs">
      <div className="text-muted-foreground text-xs font-medium uppercase tracking-wide">{label}</div>
      <div className="mt-1 text-xl font-semibold">{value}</div>
    </div>
  );
}

function Snippet({ title, code }: { title: string; code: string }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex justify-end pb-2">
          <Button type="button" variant="outline" size="sm" onClick={() => void copyToClipboard(code)}>
            <Clipboard className="h-4 w-4" />
            Copy
          </Button>
        </div>
        <pre className="overflow-x-auto rounded-md bg-zinc-950 p-4 text-xs text-zinc-100">
          <code>{code}</code>
        </pre>
      </CardContent>
    </Card>
  );
}
