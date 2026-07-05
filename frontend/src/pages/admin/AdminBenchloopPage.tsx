import {
  Archive,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Clock,
  Code,
  FileText,
  Maximize2,
  MessageSquare,
  RefreshCcw,
  Terminal,
  X,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  adminClient,
  type BenchloopLogResponse,
  type BenchloopSession,
  type BenchloopStatusResponse,
  type BenchloopTranscriptMsg,
} from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { LiveIndicator } from '@/components/admin/LiveIndicator';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import MarkdownRenderer from '@/components/common/MarkdownRenderer';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { durationLabel, formatCost, relativeTime, truncateId } from '@/lib/formatters';
import { usePolling } from '@/lib/usePolling';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function pct(n: number): string {
  return `${(n * 100).toFixed(1)}%`;
}

function delta(current: number, baseline: number, fmt: (n: number) => string, invert = false): string {
  const d = current - baseline;
  if (d === 0) return '-';
  const sign = (invert ? -d : d) > 0 ? '+' : '';
  return `${sign}${fmt(d)}`;
}

function deltaColor(current: number, baseline: number, invert = false): string {
  const d = current - baseline;
  if (d === 0) return 'text-muted-foreground';
  return (invert ? -d : d) > 0 ? 'text-emerald-600' : 'text-red-500';
}

function verdictColor(verdict: string): string {
  switch (verdict) {
    case 'continue':
      return 'border-sky-300/60 bg-sky-500/10 text-sky-700';
    case 'stop_improved':
      return 'border-emerald-300/60 bg-emerald-500/10 text-emerald-700';
    case 'rollback':
    case 'stop_no_progress':
      return 'border-red-300/60 bg-red-500/10 text-red-700';
    case 'request_matrix_change':
      return 'border-amber-300/60 bg-amber-500/10 text-amber-700';
    default:
      return '';
  }
}

function formatTime(ts: string): string {
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

/** Parse tool call text like "[Bash (`cmd`)] description" into structured parts. */
function parseToolCall(text: string): { tool: string; cmd?: string; detail: string } | null {
  const m = text.match(/^\[(\w+)(?:\s+\(`([^`]*)`\))?\]\s*(.*)/s);
  if (!m) return null;
  return { tool: m[1], cmd: m[2] || undefined, detail: m[3] || '' };
}

// ---------------------------------------------------------------------------
// Grouped message types — group consecutive tool_call/tool_result pairs
// ---------------------------------------------------------------------------

type GroupedMsg =
  | { kind: 'assistant'; msg: BenchloopTranscriptMsg }
  | { kind: 'user'; msg: BenchloopTranscriptMsg }
  | { kind: 'tool_group'; items: { call: BenchloopTranscriptMsg; result?: BenchloopTranscriptMsg }[] };

function groupMessages(msgs: BenchloopTranscriptMsg[]): GroupedMsg[] {
  const groups: GroupedMsg[] = [];
  let i = 0;
  while (i < msgs.length) {
    const msg = msgs[i];
    if (msg.role === 'tool_call') {
      // Collect consecutive tool_call/tool_result pairs
      const items: { call: BenchloopTranscriptMsg; result?: BenchloopTranscriptMsg }[] = [];
      while (i < msgs.length && (msgs[i].role === 'tool_call' || msgs[i].role === 'tool_result')) {
        if (msgs[i].role === 'tool_call') {
          const call = msgs[i];
          i++;
          // Check if next is a matching result
          if (i < msgs.length && msgs[i].role === 'tool_result') {
            items.push({ call, result: msgs[i] });
            i++;
          } else {
            items.push({ call });
          }
        } else {
          // Orphan result — skip it
          i++;
        }
      }
      if (items.length > 0) groups.push({ kind: 'tool_group', items });
    } else if (msg.role === 'user') {
      groups.push({ kind: 'user', msg });
      i++;
    } else {
      groups.push({ kind: 'assistant', msg });
      i++;
    }
  }
  return groups;
}

// ---------------------------------------------------------------------------
// Transcript rendering components
// ---------------------------------------------------------------------------

function ToolGroup({ items }: { items: { call: BenchloopTranscriptMsg; result?: BenchloopTranscriptMsg }[] }) {
  const [expanded, setExpanded] = useState(false);
  const count = items.length;

  return (
    <div className="ml-3 border-l-2 border-zinc-200/80 pl-3">
      <button
        type="button"
        className="text-muted-foreground mb-1.5 flex items-center gap-1.5 text-[11px] font-medium hover:text-zinc-700"
        onClick={() => setExpanded(!expanded)}
      >
        {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        <Code className="h-3 w-3 text-violet-400" />
        <span>
          {count} tool call{count !== 1 ? 's' : ''}
        </span>
      </button>
      {expanded && (
        <div className="space-y-1">
          {items.map((item, i) => (
            <ToolCallItem key={`tc-${item.call.timestamp || ''}-${i}`} call={item.call} result={item.result} />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolCallItem({ call, result }: { call: BenchloopTranscriptMsg; result?: BenchloopTranscriptMsg }) {
  const [showResult, setShowResult] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const parsed = parseToolCall(call.text);

  return (
    <div className="rounded-md border border-zinc-100 bg-zinc-50/60 px-2.5 py-1.5">
      <div className="flex items-center gap-2">
        <span className="rounded bg-violet-100 px-1.5 py-0.5 text-[10px] font-semibold text-violet-700">
          {parsed?.tool || 'Tool'}
        </span>
        {parsed?.cmd && (
          <code
            className={`text-muted-foreground rounded bg-zinc-100 px-1 text-[10px] ${expanded ? 'whitespace-pre-wrap break-all' : 'cursor-pointer truncate hover:text-zinc-600'}`}
            onClick={() => !expanded && setExpanded(true)}
            onKeyDown={(e) => e.key === 'Enter' && !expanded && setExpanded(true)}
            title={expanded ? undefined : 'Click to expand'}
          >
            {parsed.cmd}
          </code>
        )}
        {parsed?.detail && !parsed.cmd && (
          <span
            className={`text-muted-foreground text-[10px] ${expanded ? 'whitespace-pre-wrap break-all' : 'cursor-pointer truncate hover:text-zinc-600'}`}
            onClick={() => !expanded && setExpanded(true)}
            onKeyDown={(e) => e.key === 'Enter' && !expanded && setExpanded(true)}
            title={expanded ? undefined : 'Click to expand'}
          >
            {parsed.detail}
          </span>
        )}
        {call.timestamp && (
          <span className="text-muted-foreground ml-auto shrink-0 text-[9px]">{formatTime(call.timestamp)}</span>
        )}
      </div>
      {parsed?.cmd && parsed.detail && (
        <p
          className={`text-muted-foreground mt-0.5 text-[10px] ${expanded ? 'whitespace-pre-wrap break-all' : 'cursor-pointer truncate hover:text-zinc-600'}`}
          onClick={() => !expanded && setExpanded(true)}
          onKeyDown={(e) => e.key === 'Enter' && !expanded && setExpanded(true)}
          title={expanded ? undefined : 'Click to expand'}
        >
          {parsed.detail}
        </p>
      )}
      {expanded && (
        <button
          type="button"
          className="text-muted-foreground mt-0.5 text-[9px] hover:text-zinc-600"
          onClick={() => setExpanded(false)}
        >
          collapse
        </button>
      )}
      {result && (
        <div className="mt-1">
          <button
            type="button"
            className="text-muted-foreground flex items-center gap-0.5 text-[9px] font-medium uppercase hover:text-zinc-600"
            onClick={() => setShowResult(!showResult)}
          >
            {showResult ? <ChevronDown className="h-2 w-2" /> : <ChevronRight className="h-2 w-2" />}
            output
          </button>
          {showResult && (
            <pre className="mt-0.5 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-zinc-100/80 p-1.5 text-[10px] leading-relaxed">
              {result.text}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

function AssistantMessage({ msg }: { msg: BenchloopTranscriptMsg }) {
  return (
    <div className="rounded-lg bg-white px-4 py-3 shadow-sm ring-1 ring-zinc-100">
      {msg.timestamp && (
        <span className="text-muted-foreground mb-1 block text-[10px]">{formatTime(msg.timestamp)}</span>
      )}
      <div className="prose prose-sm prose-zinc max-w-none text-[13px] leading-relaxed [&_code]:rounded [&_code]:bg-zinc-100 [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-[11px] [&_pre]:rounded-lg [&_pre]:bg-zinc-900 [&_pre]:p-3 [&_pre]:text-[11px] [&_pre]:text-zinc-100">
        <MarkdownRenderer content={msg.text} />
      </div>
    </div>
  );
}

function UserMessage({ msg }: { msg: BenchloopTranscriptMsg }) {
  return (
    <div className="ml-8 rounded-lg border border-sky-200/60 bg-sky-50/50 px-4 py-3">
      <div className="mb-1 flex items-center gap-2">
        <span className="text-[10px] font-semibold uppercase tracking-wide text-sky-500">user</span>
        {msg.timestamp && <span className="text-muted-foreground text-[10px]">{formatTime(msg.timestamp)}</span>}
      </div>
      <div className="text-[13px] leading-relaxed">
        <MarkdownRenderer content={msg.text} />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Shared transcript message list
// ---------------------------------------------------------------------------

function TranscriptMessageList({ groups }: { groups: GroupedMsg[] }) {
  return (
    <div className="space-y-3">
      {groups.map((group, i) => {
        const key = `g-${i}`;
        if (group.kind === 'assistant') return <AssistantMessage key={key} msg={group.msg} />;
        if (group.kind === 'user') return <UserMessage key={key} msg={group.msg} />;
        return <ToolGroup key={key} items={group.items} />;
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Transcript data hook — shared between inline panel and fullscreen dialog
// ---------------------------------------------------------------------------

function useTranscriptData(sessionId: string, includeUser: boolean, includeTools: boolean, inFlight: boolean) {
  const [msgs, setMsgs] = useState<BenchloopTranscriptMsg[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchTranscript = useCallback(
    (silent = false) => {
      if (!silent) setLoading(true);
      adminClient
        .getBenchloopTranscript({
          session_id: sessionId,
          include_user: includeUser,
          include_tools: includeTools,
          max_messages: 200,
        })
        .then((res) => setMsgs(res.messages))
        .catch(() => setMsgs([]))
        .finally(() => {
          if (!silent) setLoading(false);
        });
    },
    [sessionId, includeUser, includeTools],
  );

  useEffect(() => {
    fetchTranscript();
  }, [fetchTranscript]);

  const silentRefetch = useCallback(() => fetchTranscript(true), [fetchTranscript]);
  usePolling(silentRefetch, 3000, inFlight);

  const groups = useMemo(() => groupMessages(msgs), [msgs]);

  return { msgs, groups, loading };
}

// ---------------------------------------------------------------------------
// Fullscreen transcript dialog
// ---------------------------------------------------------------------------

function TranscriptFullscreen({ session, onClose }: { session: BenchloopSession; onClose: () => void }) {
  const [includeUser, setIncludeUser] = useState(false);
  const [includeTools, setIncludeTools] = useState(true);
  const contentRef = useRef<HTMLDivElement>(null);

  const { groups, msgs, loading } = useTranscriptData(
    session.claude_session_id,
    includeUser,
    includeTools,
    session.in_flight ?? false,
  );

  // Auto-scroll
  const prevMsgCount = useRef(0);
  useEffect(() => {
    if (!loading && msgs.length > 0 && contentRef.current) {
      const el = contentRef.current;
      const isNearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 150;
      const isFirstLoad = prevMsgCount.current === 0;
      if (isFirstLoad || isNearBottom) {
        el.scrollTop = el.scrollHeight;
      }
      prevMsgCount.current = msgs.length;
    }
  }, [loading, msgs]);

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="flex h-[90vh] w-[66vw] max-w-none sm:max-w-none flex-col overflow-hidden p-0">
        <DialogHeader className="shrink-0 border-b px-6 py-4">
          <div className="flex items-center justify-between">
            <DialogTitle className="flex items-center gap-3 text-sm font-semibold">
              <MessageSquare className="h-4 w-4 text-sky-600" />
              Transcript
              <span className="text-muted-foreground font-mono text-xs font-normal">
                {truncateId(session.claude_session_id, 20)}
              </span>
              <Badge variant="outline" className="text-[10px]">
                attempt #{session.attempt} / iter {session.iteration}
              </Badge>
              {session.in_flight && <LiveIndicator label="Live" />}
            </DialogTitle>
            <div className="flex items-center gap-3">
              <label className="flex items-center gap-1.5 text-xs">
                <input
                  type="checkbox"
                  checked={includeUser}
                  onChange={(e) => setIncludeUser(e.target.checked)}
                  className="rounded"
                />
                User
              </label>
              <label className="flex items-center gap-1.5 text-xs">
                <input
                  type="checkbox"
                  checked={includeTools}
                  onChange={(e) => setIncludeTools(e.target.checked)}
                  className="rounded"
                />
                Tools
              </label>
            </div>
          </div>
        </DialogHeader>
        <div ref={contentRef} className="flex-1 overflow-y-auto px-6 py-4">
          {loading ? (
            <EmptyState message="Loading transcript..." loading />
          ) : groups.length === 0 ? (
            <EmptyState message="No messages found" />
          ) : (
            <div className="mx-auto max-w-3xl">
              <TranscriptMessageList groups={groups} />
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Inline transcript panel
// ---------------------------------------------------------------------------

function TranscriptPanel({
  session,
  onClose,
  onFullscreen,
}: {
  session: BenchloopSession;
  onClose: () => void;
  onFullscreen: () => void;
}) {
  const [includeUser, setIncludeUser] = useState(false);
  const [includeTools, setIncludeTools] = useState(true);
  const panelRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  const { groups, msgs, loading } = useTranscriptData(
    session.claude_session_id,
    includeUser,
    includeTools,
    session.in_flight ?? false,
  );

  // Scroll panel into view when it opens
  useEffect(() => {
    panelRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }, []);

  // Auto-scroll to bottom when new messages arrive, but only if user is near the bottom
  const prevMsgCount = useRef(0);
  useEffect(() => {
    if (!loading && msgs.length > 0 && contentRef.current) {
      const el = contentRef.current;
      const isNearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 150;
      const isFirstLoad = prevMsgCount.current === 0;
      if (isFirstLoad || isNearBottom) {
        el.scrollTop = el.scrollHeight;
      }
      prevMsgCount.current = msgs.length;
    }
  }, [loading, msgs]);

  return (
    <Card ref={panelRef} className="border-sky-200/60 bg-sky-50/10">
      <CardHeader className="px-5 py-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <MessageSquare className="h-4 w-4 text-sky-600" />
            <CardTitle className="text-sm font-semibold">
              Transcript
              <span className="text-muted-foreground ml-2 font-mono text-xs font-normal">
                {truncateId(session.claude_session_id, 16)}
              </span>
            </CardTitle>
            <Badge variant="outline" className="text-[10px]">
              #{session.attempt} / iter {session.iteration}
            </Badge>
            {session.in_flight && <LiveIndicator label="Live" />}
          </div>
          <div className="flex items-center gap-3">
            <label className="flex items-center gap-1.5 text-xs">
              <input
                type="checkbox"
                checked={includeUser}
                onChange={(e) => setIncludeUser(e.target.checked)}
                className="rounded"
              />
              User
            </label>
            <label className="flex items-center gap-1.5 text-xs">
              <input
                type="checkbox"
                checked={includeTools}
                onChange={(e) => setIncludeTools(e.target.checked)}
                className="rounded"
              />
              Tools
            </label>
            <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={onFullscreen} title="Fullscreen">
              <Maximize2 className="h-3.5 w-3.5" />
            </Button>
            <Button variant="ghost" size="sm" className="h-7 w-7 p-0" onClick={onClose}>
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent ref={contentRef} className="max-h-[70vh] overflow-y-auto px-5 pb-5 pt-0">
        {loading ? (
          <EmptyState message="Loading transcript..." loading />
        ) : groups.length === 0 ? (
          <EmptyState message="No messages found" />
        ) : (
          <TranscriptMessageList groups={groups} />
        )}
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function AdminBenchloopPage() {
  const [data, setData] = useState<BenchloopStatusResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Archive browsing
  const [archiveId, setArchiveId] = useState<string | null>(null);

  // Collapsible sections
  const [showMatrix, setShowMatrix] = useState(false);
  const [showMemory, setShowMemory] = useState(false);
  const [memoryContent, setMemoryContent] = useState<string | null>(null);
  const [showArchives, setShowArchives] = useState(false);

  // Inline transcript + fullscreen
  const [selectedSession, setSelectedSession] = useState<BenchloopSession | null>(null);
  const [fullscreenSession, setFullscreenSession] = useState<BenchloopSession | null>(null);

  // Log dialog
  const [logOpen, setLogOpen] = useState(false);
  const [logData, setLogData] = useState<BenchloopLogResponse | null>(null);
  const [logLoading, setLogLoading] = useState(false);

  const load = useCallback(
    (silent = false) => {
      if (!silent) setLoading(true);
      const fetcher = archiveId ? adminClient.getBenchloopArchive(archiveId) : adminClient.getBenchloopStatus();
      fetcher
        .then(setData)
        .catch((err) => setError(err.message))
        .finally(() => setLoading(false));
    },
    [archiveId],
  );

  useEffect(() => {
    load();
  }, [load]);

  const poll = useCallback(() => load(true), [load]);
  usePolling(poll, 3000, !archiveId && (data?.active ?? false));

  const selectSession = useCallback((session: BenchloopSession) => {
    setSelectedSession((prev) =>
      prev?.claude_session_id === session.claude_session_id && prev?.attempt === session.attempt ? null : session,
    );
  }, []);

  const openLog = useCallback((session: BenchloopSession) => {
    setLogOpen(true);
    setLogLoading(true);
    adminClient
      .getBenchloopLog({
        session_id: session.loop_session_id,
        attempt: session.attempt,
        tail: 300,
        log_path: session.log_path,
      })
      .then(setLogData)
      .catch(() => setLogData(null))
      .finally(() => setLogLoading(false));
  }, []);

  // Memory loader — resets and refetches when archiveId changes
  const [memoryKey, setMemoryKey] = useState(archiveId);
  useEffect(() => {
    if (archiveId !== memoryKey) {
      setMemoryContent(null);
      setMemoryKey(archiveId);
    }
  }, [archiveId, memoryKey]);

  useEffect(() => {
    if (showMemory && memoryContent === null) {
      adminClient
        .getBenchloopMemory(archiveId ? { session_id: archiveId } : undefined)
        .then((res) => setMemoryContent(res.content || '(empty)'))
        .catch(() => setMemoryContent('Failed to load memory'));
    }
  }, [showMemory, memoryContent, archiveId]);

  const state = data?.state;
  const matrix = data?.matrix;
  const decision = data?.decision;
  const sessions = data?.sessions ?? [];

  // Group sessions by loop_session_id, current session first, attempts descending within each group
  const sessionGroups = useMemo(() => {
    const map = new Map<string, BenchloopSession[]>();
    for (const s of sessions) {
      const key = s.loop_session_id || 'unknown';
      const arr = map.get(key);
      if (arr) arr.push(s);
      else map.set(key, [s]);
    }
    // Sort attempts descending within each group
    for (const arr of map.values()) {
      arr.sort((a, b) => b.attempt - a.attempt);
    }
    // Sort groups: current session first, then by most recent attempt start time descending
    const currentSessionId = state?.session_id;
    return [...map.entries()].sort(([aId, aArr], [bId, bArr]) => {
      if (aId === currentSessionId) return -1;
      if (bId === currentSessionId) return 1;
      const aTime = aArr[0]?.started_at || '';
      const bTime = bArr[0]?.started_at || '';
      return bTime.localeCompare(aTime);
    });
  }, [sessions, state?.session_id]);

  const hasMultipleSessions = sessionGroups.length > 1;

  if (loading && !data) {
    return (
      <div className="space-y-4 p-6">
        <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Benchloop' }]} />
        <EmptyState message="Loading benchloop status..." loading />
      </div>
    );
  }

  if (error && !data) {
    return (
      <div className="space-y-4 p-6">
        <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Benchloop' }]} />
        <EmptyState message={`Error: ${error}`} />
      </div>
    );
  }

  return (
    <div className="space-y-5 p-6">
      <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Benchloop' }]} />

      {/* Archive banner */}
      {archiveId && (
        <div className="flex items-center gap-3 rounded-lg border border-amber-300/40 bg-amber-50/50 px-4 py-2 text-sm">
          <Archive className="h-4 w-4 text-amber-600" />
          <span className="text-amber-800">
            Viewing archived session: <span className="font-mono font-semibold">{archiveId}</span>
          </span>
          <Button variant="outline" size="sm" className="ml-auto" onClick={() => setArchiveId(null)}>
            Back to current
          </Button>
        </div>
      )}

      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-xl font-bold tracking-tight">Benchloop</h2>
          {data?.active && <LiveIndicator />}
          {state && <StatusBadge status={state.status} />}
        </div>
        <div className="flex items-center gap-3">
          {state && (
            <span className="text-muted-foreground text-sm">
              Session <span className="font-mono text-xs">{state.session_id}</span>
              {data?.active && data.elapsed_ms > 0 && (
                <span className="ml-2">
                  <Clock className="mr-0.5 inline h-3 w-3" />
                  {durationLabel(data.elapsed_ms)}
                </span>
              )}
            </span>
          )}
          <Button variant="outline" size="sm" onClick={() => load()}>
            <RefreshCcw className="mr-1 h-3.5 w-3.5" />
            Refresh
          </Button>
        </div>
      </div>

      {!state ? (
        <EmptyState message="No active benchloop. Start a benchloop run to see status here." />
      ) : (
        <>
          {/* Live execution banner */}
          {state.live && (
            <Card className="border-sky-300/40 bg-sky-50/30">
              <CardContent className="flex flex-wrap items-center gap-x-6 gap-y-2 px-5 py-4">
                <div className="flex items-center gap-2">
                  <Badge variant="info" className="text-xs">
                    {state.live.phase || 'iteration'}
                  </Badge>
                  <span className="text-sm font-medium">{state.live.step || 'working'}</span>
                </div>
                {state.live.message && <span className="text-muted-foreground text-sm">{state.live.message}</span>}
                <div className="text-muted-foreground flex items-center gap-4 text-xs">
                  <span>
                    Iter {state.live.iteration}/{state.max_iterations}
                  </span>
                  <span>Attempt #{state.live.attempt}</span>
                  {state.live.agent_pid ? <span>PID {state.live.agent_pid}</span> : null}
                  {state.live.agent_session_id && (
                    <button
                      type="button"
                      className="font-mono text-sky-600 underline decoration-dotted"
                      onClick={() => {
                        const live = sessions.find((s) => s.claude_session_id === state.live?.agent_session_id);
                        if (live) selectSession(live);
                        else
                          selectSession({
                            loop_session_id: state.session_id,
                            phase: state.live?.phase || 'iteration',
                            iteration: state.live?.iteration || 0,
                            attempt: state.live?.attempt || 0,
                            claude_session_id: state.live?.agent_session_id || '',
                            transcript_path: state.live?.transcript_path || '',
                            log_path: state.live?.agent_log_path || '',
                            exit_code: -1,
                            started_at: state.live?.started_at || '',
                            finished_at: '',
                            in_flight: true,
                          });
                      }}
                    >
                      {truncateId(state.live.agent_session_id)}
                    </button>
                  )}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Metrics dashboard */}
          <div className="space-y-3">
            {/* Baseline vs Current */}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
              <StatCard label="Baseline Accuracy" value={pct(state.baseline_accuracy)} color="zinc" />
              <StatCard
                label="Current Accuracy"
                value={pct(state.current_accuracy)}
                color="teal"
                hint={delta(state.current_accuracy, state.baseline_accuracy, (n) => pct(n))}
              />
              <StatCard label="Baseline Parse" value={pct(state.baseline_parse_rate)} color="zinc" />
              <StatCard
                label="Current Parse"
                value={pct(state.current_parse_rate)}
                color="sky"
                hint={delta(state.current_parse_rate, state.baseline_parse_rate, (n) => pct(n))}
              />
              <StatCard label="Baseline Cost/Item" value={formatCost(state.baseline_cost_per_item)} color="zinc" />
              <StatCard
                label="Current Cost/Item"
                value={formatCost(state.current_cost_per_item)}
                color="emerald"
                hint={delta(state.current_cost_per_item, state.baseline_cost_per_item, formatCost, true)}
              />
              <StatCard label="Baseline Failed" value={state.baseline_failed_items} color="zinc" />
              <StatCard
                label="Current Failed"
                value={state.current_failed_items}
                color={state.current_failed_items > state.baseline_failed_items ? 'red' : 'emerald'}
              />
            </div>

            {/* Loop stats */}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
              <StatCard label="Iteration" value={`${state.iteration} / ${state.max_iterations}`} color="purple" />
              <StatCard label="Plateau" value={state.plateau_count} color="amber" />
              <StatCard
                label="Crashes"
                value={state.agent_crash_count}
                color={state.agent_crash_count > 0 ? 'red' : 'zinc'}
              />
              <StatCard label="Attempts" value={state.total_attempts} color="sky" />
              <StatCard label="Agent Cost" value={formatCost(state.total_agent_cost_usd)} color="emerald" />
              <StatCard
                label="Bench Cost"
                value={formatCost(state.total_bench_cost_usd)}
                color="emerald"
                hint={`Total: ${formatCost(state.total_agent_cost_usd + state.total_bench_cost_usd)}`}
              />
            </div>
          </div>

          {/* Matrix scope */}
          <Card>
            <CardHeader className="cursor-pointer px-5 py-3" onClick={() => setShowMatrix(!showMatrix)}>
              <div className="flex items-center justify-between">
                <CardTitle className="text-sm font-semibold">Matrix Scope</CardTitle>
                {showMatrix ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
              </div>
            </CardHeader>
            {showMatrix && matrix && (
              <CardContent className="space-y-3 px-5 pb-4 pt-0">
                <div className="text-muted-foreground grid grid-cols-2 gap-x-8 gap-y-1 text-sm sm:grid-cols-3">
                  <div>
                    Benchmark: <span className="text-foreground font-medium">{matrix.benchmark}</span>
                  </div>
                  <div>
                    Split: <span className="text-foreground font-medium">{matrix.split}</span>
                  </div>
                  <div>
                    Run set: <span className="text-foreground font-medium">{matrix.run_set}</span>
                  </div>
                  <div>
                    Item limit: <span className="text-foreground font-medium">{matrix.item_limit || 'all'}</span>
                  </div>
                  <div>
                    Concurrency: <span className="text-foreground font-medium">{matrix.concurrency}</span>
                  </div>
                  {matrix.approved_at && (
                    <div>
                      Approved: <span className="text-foreground font-medium">{relativeTime(matrix.approved_at)}</span>
                    </div>
                  )}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {matrix.target_workflows?.map((wf) => (
                    <Badge key={wf} variant="secondary" className="text-xs">
                      {wf}
                    </Badge>
                  ))}
                </div>
                {matrix.rationale && <p className="text-muted-foreground text-xs italic">{matrix.rationale}</p>}
              </CardContent>
            )}
            {showMatrix && !matrix && (
              <CardContent className="px-5 pb-4 pt-0">
                <span className="text-muted-foreground text-sm">No matrix lock found</span>
              </CardContent>
            )}
          </Card>

          {/* Sessions table */}
          <Card>
            <CardHeader className="px-5 py-3">
              <CardTitle className="text-sm font-semibold">Sessions / Attempts</CardTitle>
            </CardHeader>
            <CardContent className="px-0 pb-0">
              {sessions.length === 0 ? (
                <div className="px-5 pb-4">
                  <EmptyState message="No sessions recorded yet" />
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-16">#</TableHead>
                      <TableHead className="w-16">Iter</TableHead>
                      <TableHead className="w-20">Status</TableHead>
                      <TableHead>Claude Session</TableHead>
                      <TableHead className="w-24">Duration</TableHead>
                      <TableHead className="w-16">Exit</TableHead>
                      <TableHead className="w-20">Log</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sessionGroups.map(([groupId, groupSessions]) => {
                      const isCurrent = groupId === state?.session_id;
                      return [
                        // Group header row when multiple sessions exist
                        hasMultipleSessions && (
                          <TableRow key={`hdr-${groupId}`} className="bg-muted/30 hover:bg-muted/30">
                            <TableCell colSpan={7} className="py-1.5">
                              <div className="flex items-center gap-2">
                                <span className="text-muted-foreground text-[10px] font-semibold uppercase tracking-wider">
                                  Session
                                </span>
                                <span className="font-mono text-xs font-medium">{groupId}</span>
                                {isCurrent && (
                                  <Badge variant="outline" className="text-[9px]">
                                    current
                                  </Badge>
                                )}
                                <span className="text-muted-foreground text-[10px]">
                                  ({groupSessions.length} attempt{groupSessions.length !== 1 ? 's' : ''})
                                </span>
                              </div>
                            </TableCell>
                          </TableRow>
                        ),
                        // Session rows
                        ...groupSessions.map((s) => {
                          const isSelected =
                            selectedSession?.claude_session_id === s.claude_session_id &&
                            selectedSession?.attempt === s.attempt;
                          return (
                            <TableRow
                              key={`${s.loop_session_id}-${s.attempt}`}
                              className={`cursor-pointer transition-colors ${isSelected ? 'bg-sky-50/60' : 'hover:bg-muted/50'}`}
                              onClick={() => s.claude_session_id && selectSession(s)}
                            >
                              <TableCell className="font-mono text-xs">{s.attempt}</TableCell>
                              <TableCell className="text-xs">{s.iteration}</TableCell>
                              <TableCell>
                                {s.in_flight ? (
                                  <LiveIndicator label="Running" />
                                ) : s.exit_code === 0 ? (
                                  <Badge variant="success" className="text-[10px]">
                                    OK
                                  </Badge>
                                ) : (
                                  <Badge variant="destructive" className="text-[10px]">
                                    EXIT {s.exit_code}
                                  </Badge>
                                )}
                              </TableCell>
                              <TableCell className="font-mono text-xs">
                                <span className={isSelected ? 'font-semibold text-sky-700' : ''}>
                                  {s.claude_session_id ? truncateId(s.claude_session_id, 12) : '-'}
                                </span>
                              </TableCell>
                              <TableCell className="text-xs">
                                {s.duration_ms
                                  ? durationLabel(s.duration_ms)
                                  : s.started_at
                                    ? relativeTime(s.started_at)
                                    : '-'}
                              </TableCell>
                              <TableCell className="font-mono text-xs">{s.in_flight ? '-' : s.exit_code}</TableCell>
                              <TableCell>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  className="h-7 px-2 text-xs"
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    openLog(s);
                                  }}
                                >
                                  <Terminal className="mr-1 h-3 w-3" />
                                  Log
                                </Button>
                              </TableCell>
                            </TableRow>
                          );
                        }),
                      ];
                    })}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>

          {/* Inline transcript panel */}
          {selectedSession && (
            <TranscriptPanel
              session={selectedSession}
              onClose={() => setSelectedSession(null)}
              onFullscreen={() => setFullscreenSession(selectedSession)}
            />
          )}

          {/* Decision */}
          {decision && (
            <Card>
              <CardHeader className="px-5 py-3">
                <div className="flex items-center gap-3">
                  <CardTitle className="text-sm font-semibold">Last Decision</CardTitle>
                  <Badge variant="outline" className={verdictColor(decision.verdict)}>
                    {decision.verdict}
                  </Badge>
                  {decision.sanity_passed && (
                    <Badge variant="success" className="text-[10px]">
                      sanity passed
                    </Badge>
                  )}
                </div>
              </CardHeader>
              <CardContent className="space-y-3 px-5 pb-4 pt-0">
                <div className="text-muted-foreground grid grid-cols-2 gap-x-6 gap-y-1 text-sm sm:grid-cols-4">
                  <div>
                    Accuracy:{' '}
                    <span className={`font-medium ${deltaColor(decision.accuracy, state.baseline_accuracy)}`}>
                      {pct(decision.accuracy)}
                    </span>
                  </div>
                  <div>
                    Parse:{' '}
                    <span className={`font-medium ${deltaColor(decision.parse_rate, state.baseline_parse_rate)}`}>
                      {pct(decision.parse_rate)}
                    </span>
                  </div>
                  <div>
                    Cost/Item: <span className="text-foreground font-medium">{formatCost(decision.cost_per_item)}</span>
                  </div>
                  <div>
                    Failed:{' '}
                    <span className="text-foreground font-medium">
                      {decision.failed_items}/{decision.total_items}
                    </span>
                  </div>
                </div>
                {decision.changes_summary && (
                  <p className="text-sm">
                    <span className="text-muted-foreground">Changes:</span> {decision.changes_summary}
                  </p>
                )}
                {decision.reasoning && (
                  <p className="text-muted-foreground text-xs leading-relaxed">{decision.reasoning}</p>
                )}
                {decision.run_id && (
                  <Link
                    to={`/admin/benchmarks/${decision.run_id}`}
                    className="text-xs text-sky-600 underline decoration-dotted"
                  >
                    View benchmark run {truncateId(decision.run_id)}
                  </Link>
                )}
                {decision.workflows_modified?.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {decision.workflows_modified.map((wf) => (
                      <Badge key={wf} variant="secondary" className="text-xs">
                        {wf}
                      </Badge>
                    ))}
                  </div>
                )}
                {decision.failure_analysis && (
                  <details className="text-xs">
                    <summary className="text-muted-foreground cursor-pointer">Failure analysis</summary>
                    <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-zinc-50 p-2 text-[11px]">
                      {decision.failure_analysis}
                    </pre>
                  </details>
                )}
              </CardContent>
            </Card>
          )}

          {/* Loop memory */}
          <Card>
            <CardHeader className="cursor-pointer px-5 py-3" onClick={() => setShowMemory(!showMemory)}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <FileText className="h-4 w-4" />
                  <CardTitle className="text-sm font-semibold">Loop Memory</CardTitle>
                </div>
                {showMemory ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
              </div>
            </CardHeader>
            {showMemory && (
              <CardContent className="px-5 pb-4 pt-0">
                <pre className="max-h-96 overflow-auto whitespace-pre-wrap rounded bg-zinc-50 p-3 font-mono text-[11px] leading-relaxed">
                  {memoryContent || 'Loading...'}
                </pre>
              </CardContent>
            )}
          </Card>
        </>
      )}

      {/* Archives */}
      {!archiveId && data?.archived_ids && data.archived_ids.length > 0 && (
        <Card>
          <CardHeader className="cursor-pointer px-5 py-3" onClick={() => setShowArchives(!showArchives)}>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Archive className="h-4 w-4" />
                <CardTitle className="text-sm font-semibold">Archived Sessions ({data.archived_ids.length})</CardTitle>
              </div>
              {showArchives ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
            </div>
          </CardHeader>
          {showArchives && (
            <CardContent className="px-5 pb-4 pt-0">
              <div className="flex flex-wrap gap-2">
                {data.archived_ids.map((id) => (
                  <Button
                    key={id}
                    variant="outline"
                    size="sm"
                    className="font-mono text-xs"
                    onClick={() => setArchiveId(id)}
                  >
                    {id}
                  </Button>
                ))}
              </div>
            </CardContent>
          )}
        </Card>
      )}

      {/* Fullscreen transcript dialog */}
      {fullscreenSession && (
        <TranscriptFullscreen session={fullscreenSession} onClose={() => setFullscreenSession(null)} />
      )}

      {/* Log dialog */}
      <Dialog open={logOpen} onOpenChange={setLogOpen}>
        <DialogContent className="max-h-[80vh] max-w-3xl overflow-hidden">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-sm">
              <Terminal className="h-4 w-4" />
              Agent Log
              {logData && <span className="text-muted-foreground font-mono text-xs">{logData.lines} total lines</span>}
            </DialogTitle>
          </DialogHeader>
          <div className="max-h-[60vh] overflow-y-auto">
            {logLoading ? (
              <EmptyState message="Loading log..." loading />
            ) : !logData ? (
              <EmptyState message="Log not found" />
            ) : (
              <pre className="whitespace-pre-wrap rounded bg-zinc-50 p-3 font-mono text-[11px] leading-relaxed">
                {logData.content}
              </pre>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
