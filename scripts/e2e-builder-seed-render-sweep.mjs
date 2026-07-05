#!/usr/bin/env bun
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import puppeteer from 'puppeteer';

const repoRoot = new URL('..', import.meta.url).pathname.replace(/\/$/, '');
const seedDir = join(repoRoot, 'pkg/seeds/data');
const outputDir = process.env.SEED_RENDER_OUTPUT_DIR || '/tmp/consortium-seed-render-sweep';
const nodeSelector = '.react-flow__node[data-id]';
const frameSelector = '[data-testid="aggregation-frame-node"]';

function devEnvValue(key, fallback) {
  try {
    return execFileSync('./scripts/dev-env-value.sh', [key, fallback], {
      cwd: repoRoot,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
  } catch {
    return process.env[key] || fallback;
  }
}

const backendURL = process.env.BACKEND_URL || devEnvValue('BACKEND_URL', 'http://localhost:8080');
const frontendURL = process.env.FRONTEND_URL || devEnvValue('FRONTEND_URL', 'http://localhost:3000');
const dragFrameWorkflowID = process.env.SEED_RENDER_DRAG_WORKFLOW_ID || 'reasoning-peer-score-pick-cheap';

function findCachedHeadlessShell() {
  const root = join(homedir(), '.cache/puppeteer/chrome-headless-shell');
  if (!existsSync(root)) return undefined;

  const candidates = [];
  for (const revision of readdirSync(root)) {
    const revisionDir = join(root, revision);
    for (const bundle of readdirSync(revisionDir)) {
      const executable = join(revisionDir, bundle, 'chrome-headless-shell');
      if (existsSync(executable)) candidates.push({ revision, executable });
    }
  }
  candidates.sort((a, b) => a.revision.localeCompare(b.revision, undefined, { numeric: true }));
  return candidates.at(-1)?.executable;
}

function puppeteerLaunchOptions() {
  const headless = process.env.HEADLESS === 'false' ? false : true;
  const executablePath =
    process.env.PUPPETEER_EXECUTABLE_PATH || (headless ? findCachedHeadlessShell() : undefined);
  const timeout = Number.parseInt(process.env.PUPPETEER_LAUNCH_TIMEOUT_MS || '60000', 10);
  return {
    ...(executablePath ? { executablePath } : {}),
    headless,
    timeout,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-gpu', '--disable-dev-shm-usage'],
  };
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

function stableStringify(value) {
  if (Array.isArray(value)) {
    return `[${value.map((item) => stableStringify(item)).join(',')}]`;
  }
  if (value && typeof value === 'object') {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

async function fetchJSON(url, options) {
  const response = await fetch(url, options);
  const text = await response.text();
  let body;
  try {
    body = text ? JSON.parse(text) : {};
  } catch {
    body = text;
  }
  if (!response.ok) {
    throw new Error(`${url} returned ${response.status}: ${text}`);
  }
  return body;
}

async function upsertWorkflowFromSeed(workflow) {
  const workflowID = encodeURIComponent(workflow.id);
  const body = JSON.stringify(workflow);
  const updateResponse = await fetch(`${backendURL}/api/workflows/${workflowID}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body,
  });
  if (!updateResponse.ok && updateResponse.status !== 404) {
    throw new Error(`${backendURL}/api/workflows/${workflowID} returned ${updateResponse.status}: ${await updateResponse.text()}`);
  }
  if (updateResponse.status === 404) {
    await fetchJSON(`${backendURL}/api/workflows`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
    });
  } else {
    await updateResponse.text();
  }

  const saved = await fetchJSON(`${backendURL}/api/workflows/${workflowID}`);
  assert(saved.id === workflow.id, `${workflow.id}: saved workflow id mismatch ${saved.id}`);
  assert(
    stableStringify(saved.nodes || []) === stableStringify(workflow.nodes || []),
    `${workflow.id}: saved workflow nodes differ from local seed JSON`,
  );
  assert(
    stableStringify(saved.edges || []) === stableStringify(workflow.edges || []),
    `${workflow.id}: saved workflow edges differ from local seed JSON`,
  );
  return saved;
}

function readSeedWorkflows() {
  return readdirSync(seedDir)
    .filter((file) => file.endsWith('.json'))
    .sort()
    .map((file) => {
      const workflow = JSON.parse(readFileSync(join(seedDir, file), 'utf8'));
      assert(workflow.id, `${file} missing workflow id`);
      return { file, id: workflow.id, workflow };
    });
}

function nodeType(node) {
  return node?.data?.type || node?.type;
}

function expandableAggregationNodes(workflow) {
  return (workflow.nodes || []).filter(
    (node) => nodeType(node) === 'aggregation' && node?.data?.config?.aggregationWorkflowId,
  );
}

function workflowEdges(workflow) {
  return (workflow.edges || []).filter((edge) => edge?.source && edge?.target);
}

function previewRenderedEdgeID(edge) {
  const id = edge.id || `${edge.source}-${edge.target}`;
  return id.startsWith('preview-') ? id : `preview-${id}`;
}

function includesPreviewEndpoint(edge, displayIDs, parentIDs, anchorID) {
  if (edge.source === anchorID || edge.target === anchorID) {
    return false;
  }
  const sourceVisible = displayIDs.has(edge.source) || parentIDs.has(edge.source);
  const targetVisible = displayIDs.has(edge.target) || parentIDs.has(edge.target);
  return sourceVisible && targetVisible && (displayIDs.has(edge.source) || displayIDs.has(edge.target));
}

function previewEdges(preview, workflow) {
  const parentIDs = new Set((workflow.nodes || []).map((node) => node.id).filter(Boolean));
  const edges = [];
  for (const group of preview.aggregation_groups || []) {
    const displayIDs = new Set([
      ...(group.node_ids || []),
      ...(group.conditional_llm_jobs || []).map((job) => job.id),
    ].filter(Boolean));
    for (const edge of preview.edges || []) {
      if (includesPreviewEndpoint(edge, displayIDs, parentIDs, group.anchor_node_id)) {
        edges.push({ ...edge, rendered_id: previewRenderedEdgeID(edge) });
      }
    }
    for (const job of group.conditional_llm_jobs || []) {
      if (job.parent_node_id && job.id) {
        const edge = { id: `${job.parent_node_id}-${job.id}`, source: job.parent_node_id, target: job.id };
        edges.push({ ...edge, rendered_id: previewRenderedEdgeID(edge) });
      }
    }
    if (group.terminal_node_id && group.presentation_result_id) {
      const edge = {
        id: `${group.terminal_node_id}-${group.presentation_result_id}`,
        source: group.terminal_node_id,
        target: group.presentation_result_id,
      };
      edges.push({ ...edge, rendered_id: previewRenderedEdgeID(edge) });
    }
  }
  return edges.filter((edge) => edge.source && edge.target);
}

function renderedEdgeIDs(edges) {
  return [...new Set(edges.map((edge) => edge.rendered_id || edge.id || `${edge.source}-${edge.target}`))];
}

function expandedRenderedEdges(workflow, aggregations, preview) {
  const expandedAnchorIDs = new Set(aggregations.map((node) => node.id));
  const visibleWorkflowEdges = workflowEdges(workflow).filter(
    (edge) => !expandedAnchorIDs.has(edge.source) && !expandedAnchorIDs.has(edge.target),
  );
  return [...visibleWorkflowEdges, ...previewEdges(preview, workflow)];
}

function screenshotName(index, workflowID, state) {
  return `${String(index + 1).padStart(2, '0')}-${workflowID}-${state}.png`.replace(/[^a-zA-Z0-9_.-]/g, '_');
}

function htmlEscape(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function makeScreenshotPath(index, workflowID, state) {
  return join(outputDir, screenshotName(index, workflowID, state));
}

function publicArtifactPath(path) {
  return path.startsWith(outputDir) ? path.slice(outputDir.length + 1) : path;
}

function writeHTMLReport(report, path) {
  const rows = report.workflows
    .map((workflow) => {
      const screenshots = Object.entries(workflow.screenshots || {})
        .map(([state, screenshot]) =>
          screenshot
            ? `<a href="${htmlEscape(publicArtifactPath(screenshot))}">${htmlEscape(state)}</a>`
            : '',
        )
        .filter(Boolean)
        .join(' ');
      const warningCount = (workflow.states || []).reduce((sum, state) => sum + (state.warnings?.length || 0), 0);
      const states = (workflow.states || [])
        .map((state) => `${state.state}: ${state.rendered_node_count} nodes, ${state.rendered_edge_count} edges`)
        .join('<br>');
      return `<tr>
        <td>${htmlEscape(workflow.index)}</td>
        <td>${htmlEscape(workflow.id)}</td>
        <td>${htmlEscape(workflow.file)}</td>
        <td>${htmlEscape(workflow.aggregation_count)}</td>
        <td>${htmlEscape(warningCount)}</td>
        <td>${states}</td>
        <td>${screenshots}</td>
        <td class="evidence">automated geometry checks</td>
      </tr>`;
    })
    .join('\n');
  const html = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Consortium Builder Seed Render Sweep</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 24px; color: #172033; }
    table { border-collapse: collapse; width: 100%; font-size: 13px; }
    th, td { border: 1px solid #d7dce5; padding: 8px; vertical-align: top; }
    th { background: #f4f6f8; text-align: left; }
    .summary { margin-bottom: 18px; line-height: 1.5; }
    .evidence { color: #22543d; }
    a { color: #1756a9; }
  </style>
</head>
<body>
  <h1>Consortium Builder Seed Render Sweep</h1>
  <div class="summary">
    Generated: ${htmlEscape(report.generated_at)}<br>
    Frontend: ${htmlEscape(report.frontend_url)}<br>
    Backend: ${htmlEscape(report.backend_url)}<br>
    Workflows: ${htmlEscape(report.summary.workflow_count)}; expanded: ${htmlEscape(report.summary.expanded_workflow_count)}; warnings: ${htmlEscape(report.summary.warning_count)}
  </div>
  <table>
    <thead>
      <tr>
        <th>#</th><th>Workflow</th><th>Seed File</th><th>Aggregations</th><th>Warnings</th><th>Rendered States</th><th>Screenshots</th><th>Evidence</th>
      </tr>
    </thead>
    <tbody>${rows}</tbody>
  </table>
</body>
</html>
`;
  writeFileSync(path, html);
}

async function waitForWorkflowNodes(page, expectedCount, label) {
  await page.waitForFunction(
    (selector, count) => document.querySelectorAll(selector).length >= count,
    { timeout: 30000 },
    nodeSelector,
    expectedCount,
  );
  await page.waitForFunction(() => !document.body.innerText.includes('Loading workflow'), { timeout: 30000 });
  await new Promise((resolve) => setTimeout(resolve, 1200));

  const renderedCount = await page.$$eval(nodeSelector, (nodes) => nodes.length);
  assert(renderedCount >= expectedCount, `${label}: expected at least ${expectedCount} rendered nodes, got ${renderedCount}`);
}

async function clickFlowNode(page, nodeID, label) {
  const point = await page.evaluate(
    (selector, id) => {
      const node = [...document.querySelectorAll(selector)].find((el) => el.getAttribute('data-id') === id);
      if (!node) return null;
      const rect = node.getBoundingClientRect();
      return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
    },
    nodeSelector,
    nodeID,
  );
  assert(point, `${label}: could not find React Flow node ${nodeID}`);
  await page.mouse.click(point.x, point.y);
}

async function validateReadOnlyAggregationPanel(page, label) {
  const report = await page.evaluate(() => {
    const visible = (el) => {
      const rect = el.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0;
    };
    const editableControls = [...document.querySelectorAll('input[id^="config-"], select[id^="config-"], textarea[id^="config-"]')]
      .filter((el) => visible(el) && !el.disabled && !el.readOnly)
      .map((el) => el.id || el.getAttribute('aria-label') || el.tagName.toLowerCase());
    const deleteVisible = [...document.querySelectorAll('button')].some(
      (button) => visible(button) && button.textContent?.trim() === 'Delete Node',
    );
    return {
      hasReadOnlyNotice: document.body.innerText.includes('Expanded aggregation previews are read-only'),
      editableControls,
      deleteVisible,
    };
  });
  assert(report.hasReadOnlyNotice, `${label}: selected expanded aggregation is missing read-only notice`);
  assert(
    report.editableControls.length === 0,
    `${label}: selected expanded aggregation shows editable config controls ${report.editableControls.join(', ')}`,
  );
  assert(!report.deleteVisible, `${label}: selected expanded aggregation still shows Delete Node`);
}

async function validateGeometry(page, { label, expectedNodeIDs, edges, renderedEdges = edges, frames = [] }) {
  const report = await page.evaluate(
    ({ selector, frameSelectorArg, expectedIDs, expectedEdges, expectedRenderedEdgeIDs, expectedFrames }) => {
      const graphRect = document.querySelector('.react-flow')?.getBoundingClientRect();
      const viewportEl = document.querySelector('.react-flow__viewport');
      const transform = viewportEl ? getComputedStyle(viewportEl).transform : '';
      let zoom = null;
      if (transform && transform !== 'none') {
        const matrix = transform.match(/matrix\(([^)]+)\)/);
        const values = matrix ? matrix[1].split(',').map((value) => Number.parseFloat(value.trim())) : [];
        if (Number.isFinite(values[0])) {
          zoom = values[0];
        }
      }
      const renderedEdgeIDs = new Set(
        [...document.querySelectorAll('.react-flow__edge[data-id]')]
          .map((el) => el.getAttribute('data-id'))
          .filter(Boolean),
      );
      const rectByID = {};
      const labelClips = [];
      for (const el of document.querySelectorAll(selector)) {
        const id = el.getAttribute('data-id');
        if (!id) continue;
        const rect = el.getBoundingClientRect();
        const contentEl = el.querySelector('[data-testid^="workflow-node-"], [data-testid="aggregation-frame-node"]') || el;
        const clipTolerance = 6;
        if (
          expectedIDs.includes(id) &&
          (contentEl.scrollWidth > contentEl.clientWidth + clipTolerance ||
            contentEl.scrollHeight > contentEl.clientHeight + clipTolerance)
        ) {
          labelClips.push({
            id,
            scrollWidth: contentEl.scrollWidth,
            clientWidth: contentEl.clientWidth,
            scrollHeight: contentEl.scrollHeight,
            clientHeight: contentEl.clientHeight,
          });
        }
        rectByID[id] = {
          id,
          left: rect.left,
          right: rect.right,
          top: rect.top,
          bottom: rect.bottom,
          width: rect.width,
          height: rect.height,
          centerX: rect.left + rect.width / 2,
          centerY: rect.top + rect.height / 2,
          isFrame: Boolean(el.querySelector(frameSelectorArg)),
          text: el.textContent || '',
        };
      }

      const missing = expectedIDs.filter((id) => !rectByID[id]);
      const missingRenderedEdges = expectedRenderedEdgeIDs.filter((id) => !renderedEdgeIDs.has(id));
      const tolerance = 12;
      const outsideGraph = [];
      if (graphRect) {
        for (const id of expectedIDs) {
          const rect = rectByID[id];
          if (!rect) continue;
          if (
            rect.left < graphRect.left - tolerance ||
            rect.right > graphRect.right + tolerance ||
            rect.top < graphRect.top - tolerance ||
            rect.bottom > graphRect.bottom + tolerance
          ) {
            outsideGraph.push(id);
          }
        }
      }

      const badEdges = [];
      const longEdges = [];
      for (const edge of expectedEdges) {
        const source = rectByID[edge.source];
        const target = rectByID[edge.target];
        if (!source || !target) continue;
        const dx = target.centerX - source.centerX;
        const dy = target.centerY - source.centerY;
        const distance = Math.round(Math.sqrt(dx * dx + dy * dy));
        if (distance > 950 || Math.abs(dy) > 850) {
          longEdges.push({
            source: edge.source,
            target: edge.target,
            distance,
            vertical: Math.round(Math.abs(dy)),
          });
        }
        if (target.centerY <= source.centerY + 2) {
          badEdges.push({
            source: edge.source,
            target: edge.target,
            sourceY: Math.round(source.centerY),
            targetY: Math.round(target.centerY),
          });
        }
      }

      const orientation = (a, b, c) => (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x);
      const segmentsIntersect = (a, b, c, d) => {
        const abC = orientation(a, b, c);
        const abD = orientation(a, b, d);
        const cdA = orientation(c, d, a);
        const cdB = orientation(c, d, b);
        return abC * abD < 0 && cdA * cdB < 0;
      };
      const edgeSegments = expectedEdges
        .map((edge) => {
          const source = rectByID[edge.source];
          const target = rectByID[edge.target];
          if (!source || !target) return null;
          return {
            source: edge.source,
            target: edge.target,
            a: { x: source.centerX, y: source.centerY },
            b: { x: target.centerX, y: target.centerY },
          };
        })
        .filter(Boolean);
      const edgeCrossings = [];
      for (let i = 0; i < edgeSegments.length; i += 1) {
        for (let j = i + 1; j < edgeSegments.length; j += 1) {
          const first = edgeSegments[i];
          const second = edgeSegments[j];
          if (
            first.source === second.source ||
            first.source === second.target ||
            first.target === second.source ||
            first.target === second.target
          ) {
            continue;
          }
          if (segmentsIntersect(first.a, first.b, second.a, second.b)) {
            edgeCrossings.push({
              first: `${first.source}->${first.target}`,
              second: `${second.source}->${second.target}`,
            });
          }
        }
      }

      const overlapPairs = [];
      const containedFramePairs = new Set();
      for (const frame of expectedFrames) {
        for (const childID of frame.children) {
          containedFramePairs.add(`${frame.id}->${childID}`);
          containedFramePairs.add(`${childID}->${frame.id}`);
        }
      }
      const rects = expectedIDs.map((id) => rectByID[id]).filter(Boolean);
      for (let i = 0; i < rects.length; i += 1) {
        for (let j = i + 1; j < rects.length; j += 1) {
          if (containedFramePairs.has(`${rects[i].id}->${rects[j].id}`)) {
            continue;
          }
          const left = Math.max(rects[i].left, rects[j].left);
          const right = Math.min(rects[i].right, rects[j].right);
          const top = Math.max(rects[i].top, rects[j].top);
          const bottom = Math.min(rects[i].bottom, rects[j].bottom);
          if (right - left > 3 && bottom - top > 3) {
            overlapPairs.push([rects[i].id, rects[j].id]);
          }
        }
      }

      const frameReports = expectedFrames.map((frame) => {
        const frameRect = rectByID[frame.id];
        const children = frame.children.map((id) => rectByID[id]).filter(Boolean);
        const outside = [];
        if (frameRect) {
          for (const child of children) {
            if (
              child.left < frameRect.left - tolerance ||
              child.right > frameRect.right + tolerance ||
              child.top < frameRect.top - tolerance ||
              child.bottom > frameRect.bottom + tolerance
            ) {
              outside.push(child.id);
            }
          }
        }
        return {
          id: frame.id,
          hasFrame: Boolean(frameRect),
          expectedChildren: frame.children.length,
          renderedChildren: children.length,
          llmJobCount: frame.llmJobCount,
          text: frameRect?.text || '',
          outside,
        };
      });

      const rectsForBounds = expectedIDs.map((id) => rectByID[id]).filter(Boolean);
      const graphBounds =
        rectsForBounds.length > 0
          ? {
              left: Math.round(Math.min(...rectsForBounds.map((rect) => rect.left))),
              top: Math.round(Math.min(...rectsForBounds.map((rect) => rect.top))),
              right: Math.round(Math.max(...rectsForBounds.map((rect) => rect.right))),
              bottom: Math.round(Math.max(...rectsForBounds.map((rect) => rect.bottom))),
              width: Math.round(
                Math.max(...rectsForBounds.map((rect) => rect.right)) -
                  Math.min(...rectsForBounds.map((rect) => rect.left)),
              ),
              height: Math.round(
                Math.max(...rectsForBounds.map((rect) => rect.bottom)) -
                  Math.min(...rectsForBounds.map((rect) => rect.top)),
              ),
            }
          : null;
      const warnings = [];
      if (Number.isFinite(zoom) && zoom < 0.35) {
        warnings.push({ code: 'low_zoom', message: `Zoom ${zoom.toFixed(2)} may reduce readability` });
      }
      if (labelClips.length > 0) {
        warnings.push({ code: 'label_clipping', message: `${labelClips.length} nodes may clip text`, details: labelClips });
      }
      if (longEdges.length > 0) {
        warnings.push({ code: 'long_edges', message: `${longEdges.length} long edges detected`, details: longEdges });
      }
      if (edgeCrossings.length > 0) {
        warnings.push({
          code: 'edge_crossings',
          message: `${edgeCrossings.length} center-line edge crossings detected`,
          details: edgeCrossings.slice(0, 50),
        });
      }

      return {
        hasGraph: Boolean(graphRect),
        viewport: graphRect
          ? {
              width: Math.round(graphRect.width),
              height: Math.round(graphRect.height),
            }
          : null,
        zoom,
        graphBounds,
        renderedNodeCount: Object.keys(rectByID).length,
        renderedEdgeCount: renderedEdgeIDs.size,
        missing,
        missingRenderedEdges,
        outsideGraph,
        badEdges,
        longEdges,
        edgeCrossings,
        labelClips,
        overlapPairs,
        frameReports,
        warnings,
      };
    },
    {
      selector: nodeSelector,
      frameSelectorArg: frameSelector,
      expectedIDs: expectedNodeIDs,
      expectedEdges: edges,
      expectedRenderedEdgeIDs: renderedEdgeIDs(renderedEdges),
      expectedFrames: frames,
    },
  );

  assert(report.hasGraph, `${label}: React Flow graph is missing`);
  assert(report.missing.length === 0, `${label}: missing rendered nodes ${report.missing.join(', ')}`);
  assert(
    report.missingRenderedEdges.length === 0,
    `${label}: missing rendered edges ${report.missingRenderedEdges.join(', ')}`,
  );
  assert(
    report.outsideGraph.length === 0,
    `${label}: rendered nodes outside graph viewport ${report.outsideGraph.join(', ')}`,
  );
  assert(report.badEdges.length === 0, `${label}: edges not top-to-bottom ${JSON.stringify(report.badEdges)}`);
  assert(report.overlapPairs.length === 0, `${label}: overlapping rendered nodes ${JSON.stringify(report.overlapPairs)}`);
  for (const frame of report.frameReports) {
    assert(frame.hasFrame, `${label}: missing expanded frame ${frame.id}`);
    assert(
      frame.renderedChildren === frame.expectedChildren,
      `${label}: frame ${frame.id} rendered ${frame.renderedChildren}/${frame.expectedChildren} expected children`,
    );
    assert(frame.outside.length === 0, `${label}: frame ${frame.id} has children outside frame ${frame.outside.join(', ')}`);
    if (Number.isFinite(frame.llmJobCount)) {
      assert(
        frame.text.includes(`${frame.llmJobCount} LLM jobs`),
        `${label}: frame ${frame.id} is missing backend LLM job count badge ${frame.llmJobCount}`,
      );
    }
  }
  return report;
}

async function dragExpandedFrame(page, { label, frameID, childID }) {
  const before = await page.evaluate(
    ({ selector, frameSelectorArg, frameIDArg, childIDArg }) => {
      const frame = [...document.querySelectorAll(frameSelectorArg)].find(
        (el) => el.getAttribute('data-node-id') === frameIDArg,
      );
      const child = [...document.querySelectorAll(selector)].find((el) => el.getAttribute('data-id') === childIDArg);
      if (!frame || !child) return null;
      const frameRect = frame.getBoundingClientRect();
      const childRect = child.getBoundingClientRect();
      return {
        frameLeft: frameRect.left,
        frameTop: frameRect.top,
        dragX: frameRect.left + 32,
        dragY: frameRect.top + 24,
        childLeft: childRect.left,
        childTop: childRect.top,
      };
    },
    { selector: nodeSelector, frameSelectorArg: frameSelector, frameIDArg: frameID, childIDArg: childID },
  );
  assert(before, `${label}: could not find frame ${frameID} and child ${childID} for drag validation`);

  await page.mouse.move(before.dragX, before.dragY);
  await page.mouse.down();
  await page.mouse.move(before.dragX + 140, before.dragY + 70, { steps: 18 });
  await page.mouse.up();

  const after = await page.waitForFunction(
    ({ selector, frameSelectorArg, frameIDArg, childIDArg, previous }) => {
      const frame = [...document.querySelectorAll(frameSelectorArg)].find(
        (el) => el.getAttribute('data-node-id') === frameIDArg,
      );
      const child = [...document.querySelectorAll(selector)].find((el) => el.getAttribute('data-id') === childIDArg);
      if (!frame || !child) return false;
      const frameRect = frame.getBoundingClientRect();
      const childRect = child.getBoundingClientRect();
      const frameMoved = Math.abs(frameRect.left - previous.frameLeft) > 20 || Math.abs(frameRect.top - previous.frameTop) > 20;
      const childMoved = Math.abs(childRect.left - previous.childLeft) > 20 || Math.abs(childRect.top - previous.childTop) > 20;
      return frameMoved && childMoved
        ? {
            frameLeft: frameRect.left,
            frameTop: frameRect.top,
            childLeft: childRect.left,
            childTop: childRect.top,
          }
        : false;
    },
    { timeout: 30000 },
    { selector: nodeSelector, frameSelectorArg: frameSelector, frameIDArg: frameID, childIDArg: childID, previous: before },
  );
  assert(await after.jsonValue(), `${label}: dragging frame ${frameID} did not move its internals`);
}

async function dragExpandedInternalNode(page, { label, childID }) {
  const before = await page.evaluate(
    ({ selector, childIDArg }) => {
      const child = [...document.querySelectorAll(selector)].find((el) => el.getAttribute('data-id') === childIDArg);
      if (!child) return null;
      const rect = child.getBoundingClientRect();
      return {
        left: rect.left,
        top: rect.top,
        centerX: rect.left + rect.width / 2,
        centerY: rect.top + rect.height / 2,
      };
    },
    { selector: nodeSelector, childIDArg: childID },
  );
  assert(before, `${label}: could not find expanded internal node ${childID} for drag validation`);

  await page.mouse.move(before.centerX, before.centerY);
  await page.mouse.down();
  await page.mouse.move(before.centerX + 80, before.centerY + 50, { steps: 14 });
  await page.mouse.up();

  const after = await page.waitForFunction(
    ({ selector, childIDArg, previous }) => {
      const child = [...document.querySelectorAll(selector)].find((el) => el.getAttribute('data-id') === childIDArg);
      if (!child) return false;
      const rect = child.getBoundingClientRect();
      return Math.abs(rect.left - previous.left) > 20 || Math.abs(rect.top - previous.top) > 20
        ? { left: rect.left, top: rect.top }
        : false;
    },
    { timeout: 30000 },
    { selector: nodeSelector, childIDArg: childID, previous: before },
  );
  assert(await after.jsonValue(), `${label}: dragging internal node ${childID} did not move it`);
}

async function main() {
  await fetchJSON(`${backendURL}/health`);
  mkdirSync(outputDir, { recursive: true });

  const seeds = readSeedWorkflows();
  const browser = await puppeteer.launch(puppeteerLaunchOptions());
  const page = await browser.newPage();
  await page.setViewport({ width: 1800, height: 1200, deviceScaleFactor: 1 });
  page.setDefaultTimeout(30000);
  await page.evaluateOnNewDocument(() => {
    localStorage.clear();
  });
  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      console.error(`[browser] ${msg.text()}`);
    }
  });

  const report = {
    schema_version: 1,
    generated_at: new Date().toISOString(),
    repo_root: repoRoot,
    backend_url: backendURL,
    frontend_url: frontendURL,
    output_dir: outputDir,
    viewport: { width: 1800, height: 1200, device_scale_factor: 1 },
    summary: {
      workflow_count: seeds.length,
      expanded_workflow_count: 0,
      expanded_group_count: 0,
      warning_count: 0,
      screenshot_count: 0,
    },
    workflows: [],
  };

  let expandedWorkflowCount = 0;
  let draggedFrame = false;
  let draggedInternalNode = false;
  try {
    for (const [index, seed] of seeds.entries()) {
      const workflow = await upsertWorkflowFromSeed(seed.workflow);
      const nodeIDs = (workflow.nodes || []).map((node) => node.id).filter(Boolean);
      const aggregations = expandableAggregationNodes(workflow);
      const label = `${seed.id} (${index + 1}/${seeds.length})`;
      const workflowReport = {
        index: index + 1,
        id: seed.id,
        file: seed.file,
        node_count: nodeIDs.length,
        edge_count: workflowEdges(workflow).length,
        aggregation_count: aggregations.length,
        states: [],
        preview_groups: [],
        screenshots: {},
        review_evidence: {
          automated_geometry: 'passed',
          external_reviews: 'not recorded by this script',
        },
      };

      await page.goto(`${frontendURL}/workflow/${seed.id}`, { waitUntil: 'networkidle0' });
      await waitForWorkflowNodes(page, nodeIDs.length, label);
      const collapsedGeometry = await validateGeometry(page, {
        label: `${label} collapsed`,
        expectedNodeIDs: nodeIDs,
        edges: workflowEdges(workflow),
        renderedEdges: workflowEdges(workflow),
      });
      workflowReport.states.push({
        state: 'collapsed',
        rendered_node_count: collapsedGeometry.renderedNodeCount,
        rendered_edge_count: collapsedGeometry.renderedEdgeCount,
        viewport: collapsedGeometry.viewport,
        zoom: collapsedGeometry.zoom,
        graph_bounds: collapsedGeometry.graphBounds,
        warnings: collapsedGeometry.warnings,
      });
      const collapsedScreenshot = makeScreenshotPath(index, seed.id, 'collapsed');
      await page.screenshot({ path: collapsedScreenshot });
      workflowReport.screenshots.collapsed = collapsedScreenshot;

      if (aggregations.length === 0) {
        report.workflows.push(workflowReport);
        continue;
      }

      const preview = await fetchJSON(`${backendURL}/api/workflows/compile-preview`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ workflow_file: workflow }),
      });
      const groupsByAnchor = new Map((preview.aggregation_groups || []).map((group) => [group.anchor_node_id, group]));
      const expandedFrames = [];
      const expandedNodeIDs = [...nodeIDs];

      for (const aggregation of aggregations) {
        const group = groupsByAnchor.get(aggregation.id);
        assert(group, `${label}: compile preview omitted aggregation group for ${aggregation.id}`);
        assert(group.llm_job_count >= 0, `${label}: group ${aggregation.id} missing LLM job count`);

        await clickFlowNode(page, aggregation.id, label);
        await page.waitForSelector('[data-testid="aggregation-expand-button"]:not([disabled])');
        await page.click('[data-testid="aggregation-expand-button"]');
        const frameID = `${aggregation.id}--frame`;
        await page.waitForFunction(
          (selector, id) => [...document.querySelectorAll(selector)].some((el) => el.getAttribute('data-id') === id),
          { timeout: 30000 },
          nodeSelector,
          frameID,
        );
        await page.waitForFunction((text) => document.body.innerText.includes(text), { timeout: 30000 }, 'Compiled preview');
        await validateReadOnlyAggregationPanel(page, `${label} ${aggregation.id}`);

        const children = [
          ...(group.node_ids || []),
          ...(group.conditional_llm_jobs || []).map((job) => job.id),
        ].filter(Boolean);
        expandedNodeIDs.push(frameID, ...children);
        expandedFrames.push({ id: frameID, children, llmJobCount: group.llm_job_count });
        workflowReport.preview_groups.push({
          anchor_node_id: group.anchor_node_id,
          method: group.method,
          source_workflow_id: group.source_workflow_id,
          terminal_node_id: group.terminal_node_id,
          presentation_result_id: group.presentation_result_id,
          input_node_ids: group.input_node_ids || [],
          node_count: (group.node_ids || []).length,
          llm_job_count: group.llm_job_count,
          top_level_llm_job_count: group.top_level_llm_job_count,
          conditional_llm_job_count: group.conditional_llm_job_count,
          conditional_llm_jobs: group.conditional_llm_jobs || [],
          operation_count: group.operation_count,
        });
      }

      await new Promise((resolve) => setTimeout(resolve, 300));
      const expectedExpandedRenderedEdges = expandedRenderedEdges(workflow, aggregations, preview);
      const expandedGeometry = await validateGeometry(page, {
        label: `${label} expanded`,
        expectedNodeIDs: [...new Set(expandedNodeIDs)],
        edges: expectedExpandedRenderedEdges,
        renderedEdges: expectedExpandedRenderedEdges,
        frames: expandedFrames,
      });
      workflowReport.states.push({
        state: 'expanded',
        rendered_node_count: expandedGeometry.renderedNodeCount,
        rendered_edge_count: expandedGeometry.renderedEdgeCount,
        viewport: expandedGeometry.viewport,
        zoom: expandedGeometry.zoom,
        graph_bounds: expandedGeometry.graphBounds,
        warnings: expandedGeometry.warnings,
        frames: expandedGeometry.frameReports,
      });
      const expandedScreenshot = makeScreenshotPath(index, seed.id, 'expanded');
      await page.screenshot({ path: expandedScreenshot });
      workflowReport.screenshots.expanded = expandedScreenshot;

      if (!draggedFrame && (seed.id === dragFrameWorkflowID || index === seeds.length - 1)) {
        const frame = expandedFrames.find((candidate) => candidate.children.length > 0);
        assert(frame, `${label}: no expanded frame with children available for drag validation`);
        await dragExpandedInternalNode(page, {
          label,
          childID: frame.children[0],
        });
        const internalDragGeometry = await validateGeometry(page, {
          label: `${label} internal-node-drag`,
          expectedNodeIDs: [...new Set(expandedNodeIDs)],
          edges: expectedExpandedRenderedEdges,
          renderedEdges: expectedExpandedRenderedEdges,
          frames: expandedFrames,
        });
        workflowReport.states.push({
          state: 'expanded-internal-drag',
          rendered_node_count: internalDragGeometry.renderedNodeCount,
          rendered_edge_count: internalDragGeometry.renderedEdgeCount,
          viewport: internalDragGeometry.viewport,
          zoom: internalDragGeometry.zoom,
          graph_bounds: internalDragGeometry.graphBounds,
          warnings: internalDragGeometry.warnings,
          frames: internalDragGeometry.frameReports,
        });
        const internalDragScreenshot = makeScreenshotPath(index, seed.id, 'expanded-internal-drag');
        await page.screenshot({ path: internalDragScreenshot });
        workflowReport.screenshots.expanded_internal_drag = internalDragScreenshot;
        draggedInternalNode = true;

        await dragExpandedFrame(page, {
          label,
          frameID: frame.id,
          childID: frame.children[0],
        });
        const frameDragGeometry = await validateGeometry(page, {
          label: `${label} frame-drag`,
          expectedNodeIDs: [...new Set(expandedNodeIDs)],
          edges: expectedExpandedRenderedEdges,
          renderedEdges: expectedExpandedRenderedEdges,
          frames: expandedFrames,
        });
        workflowReport.states.push({
          state: 'expanded-frame-drag',
          rendered_node_count: frameDragGeometry.renderedNodeCount,
          rendered_edge_count: frameDragGeometry.renderedEdgeCount,
          viewport: frameDragGeometry.viewport,
          zoom: frameDragGeometry.zoom,
          graph_bounds: frameDragGeometry.graphBounds,
          warnings: frameDragGeometry.warnings,
          frames: frameDragGeometry.frameReports,
        });
        const frameDragScreenshot = makeScreenshotPath(index, seed.id, 'expanded-frame-drag');
        await page.screenshot({ path: frameDragScreenshot });
        workflowReport.screenshots.expanded_frame_drag = frameDragScreenshot;
        draggedFrame = true;
      }
      expandedWorkflowCount += 1;
      report.workflows.push(workflowReport);
    }
  } finally {
    await browser.close();
  }

  assert(draggedFrame, `did not exercise expanded frame drag; target workflow=${dragFrameWorkflowID}`);
  assert(draggedInternalNode, `did not exercise expanded internal node drag; target workflow=${dragFrameWorkflowID}`);
  report.summary.expanded_workflow_count = expandedWorkflowCount;
  report.summary.expanded_group_count = report.workflows.reduce(
    (sum, workflow) => sum + (workflow.preview_groups?.length || 0),
    0,
  );
  report.summary.warning_count = report.workflows.reduce(
    (sum, workflow) => sum + workflow.states.reduce((stateSum, state) => stateSum + (state.warnings?.length || 0), 0),
    0,
  );
  report.summary.screenshot_count = report.workflows.reduce(
    (sum, workflow) => sum + Object.values(workflow.screenshots || {}).filter(Boolean).length,
    0,
  );
  const reportJSONPath = join(outputDir, 'report.json');
  const reportHTMLPath = join(outputDir, 'report.html');
  writeFileSync(reportJSONPath, JSON.stringify(report, null, 2));
  writeHTMLReport(report, reportHTMLPath);
  console.log(
    `seed render sweep passed: ${seeds.length} workflows, ${expandedWorkflowCount} expanded; screenshots=${outputDir}; report=${reportJSONPath}`,
  );
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
