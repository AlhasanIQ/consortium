#!/usr/bin/env bun
import { execFileSync } from 'node:child_process';
import { existsSync, readdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import puppeteer from 'puppeteer';

const repoRoot = new URL('..', import.meta.url).pathname.replace(/\/$/, '');

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
const workflowID = 'reasoning-informed-captain-synthesis-cheap';
const jobTimeoutMs = Number.parseInt(process.env.BUILDER_E2E_JOB_TIMEOUT_MS || '600000', 10);
const frameSelector = '[data-testid="aggregation-frame-node"]';
const previewNodeSelector =
  '[data-testid="workflow-node-agent"], [data-testid="workflow-node-operation"], [data-testid="workflow-node-conditional"]';

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

async function waitForJob(jobID, timeoutMs = jobTimeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    last = await fetchJSON(`${backendURL}/api/jobs/${jobID}`);
    if (['completed', 'failed', 'cancelled'].includes(last.status)) {
      return last;
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error(`job ${jobID} did not finish before timeout; last=${JSON.stringify(last)}`);
}

async function compilePreviewForWorkflow(workflowID) {
  const seed = await fetchJSON(`${backendURL}/api/workflows/${workflowID}`);
  const preview = await fetchJSON(`${backendURL}/api/workflows/compile-preview`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ workflow_file: seed }),
  });
  const group = preview.aggregation_groups?.[0];
  assert(group, `${workflowID} compile preview missing aggregation group: ${JSON.stringify(preview)}`);
  return { seed, seedJSON: JSON.stringify(seed), preview, group };
}

function assertPreviewCounts(workflowID, group, { llmJobs, topLevelLLMJobs, conditionalLLMJobs }) {
  assert(group.llm_job_count === llmJobs, `${workflowID} expected ${llmJobs} LLM jobs, got ${group.llm_job_count}`);
  assert(
    group.top_level_llm_job_count === topLevelLLMJobs,
    `${workflowID} expected ${topLevelLLMJobs} top-level LLM jobs, got ${group.top_level_llm_job_count}`,
  );
  assert(
    group.conditional_llm_job_count === conditionalLLMJobs,
    `${workflowID} expected ${conditionalLLMJobs} conditional LLM jobs, got ${group.conditional_llm_job_count}`,
  );
}

function aggregationNodes(workflowFile) {
  return (workflowFile.nodes || []).filter((node) => {
    const type = node?.data?.type || node?.type;
    return type === 'aggregation';
  });
}

function stableJSON(value) {
  return JSON.stringify(value);
}

function nodeMetadata(node) {
  const raw = node?.metadata ?? node?.Metadata;
  if (!raw) return {};
  if (typeof raw === 'string') {
    try {
      return JSON.parse(raw);
    } catch {
      return {};
    }
  }
  return raw;
}

function hasCompiledSourceNode(nodes) {
  return (nodes || []).some((node) => {
    const metadata = nodeMetadata(node);
    return node.node_id?.includes('--') && typeof metadata.source_workflow_id === 'string';
  });
}

async function assertExpandedPreviewVisible(page, label) {
  const deadline = Date.now() + 4000;
  let visibility;
  while (Date.now() < deadline) {
    visibility = await page.evaluate((frameSelectorArg, nodeSelectorArg) => {
      const graph = document.querySelector('.react-flow')?.getBoundingClientRect();
      const previewNodes = [...document.querySelectorAll(nodeSelectorArg)]
        .filter((el) => el.textContent?.includes('Compiled preview'))
        .map((el) => el.getBoundingClientRect());
      const frames = [...document.querySelectorAll(frameSelectorArg)].map((el) => el.getBoundingClientRect());
      const tolerance = 4;
      const insideGraph = (rect) =>
        Boolean(graph) &&
        rect.left >= graph.left - tolerance &&
        rect.right <= graph.right + tolerance &&
        rect.top >= graph.top - tolerance &&
        rect.bottom <= graph.bottom + tolerance;
      return {
        hasGraph: Boolean(graph),
        frames: frames.length,
        previewNodes: previewNodes.length,
        allInsideGraph:
          Boolean(graph) &&
          frames.length > 0 &&
          previewNodes.length > 0 &&
          frames.every(insideGraph) &&
          previewNodes.every(insideGraph),
      };
    }, frameSelector, previewNodeSelector);
    if (visibility.allInsideGraph) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  assert(
    false,
    `${label} expanded preview is not fully visible in the graph viewport: ${JSON.stringify(visibility)}`,
  );
}

async function expandAggregationPreview(page, workflowID, expectedLLMJobs) {
  await page.goto(`${frontendURL}/workflow/${workflowID}`, { waitUntil: 'networkidle0' });
  await page.waitForSelector('[data-testid="workflow-node-aggregation"]');
  await page.click('[data-testid="workflow-node-aggregation"]');
  await page.waitForSelector('[data-testid="aggregation-expand-button"]:not([disabled])');
  await page.click('[data-testid="aggregation-expand-button"]');
  await page.waitForSelector(frameSelector);
  await page.waitForFunction((text) => document.body.innerText.includes(text), {}, `${expectedLLMJobs} LLM jobs`);
  await page.waitForFunction(() => document.body.innerText.includes('Compiled preview'));
}

async function assertFrameContainsPreviewNodes(page, label, minPreviewNodes = 1) {
  const geometry = await page.evaluate((frameSelectorArg, nodeSelectorArg) => {
    const frame = document.querySelector(frameSelectorArg)?.getBoundingClientRect();
    const previewNodes = [...document.querySelectorAll(nodeSelectorArg)]
      .filter((el) => el.textContent?.includes('Compiled preview'))
      .map((el) => el.getBoundingClientRect());
    const tolerance = 4;
    return {
      hasFrame: Boolean(frame),
      previewNodes: previewNodes.length,
      allInsideFrame:
        Boolean(frame) &&
        previewNodes.length > 0 &&
        previewNodes.every(
          (rect) =>
            rect.left >= frame.left - tolerance &&
            rect.right <= frame.right + tolerance &&
            rect.top >= frame.top - tolerance &&
            rect.bottom <= frame.bottom + tolerance,
        ),
    };
  }, frameSelector, previewNodeSelector);
  assert(geometry.hasFrame, `${label} expanded preview frame is missing`);
  assert(
    geometry.previewNodes >= minPreviewNodes,
    `${label} expected at least ${minPreviewNodes} preview nodes inside the frame, got ${geometry.previewNodes}`,
  );
  assert(geometry.allInsideFrame, `${label} rendered one or more preview nodes outside the aggregation frame`);
}

async function assertPreviewEdgeDeleteIgnored(page, label) {
  const target = await page.evaluate(() => {
    const edges = [...document.querySelectorAll('.react-flow__edge[data-id^="preview-"]')].filter((edge) => {
      const id = edge.getAttribute('data-id') || '';
      return !id.includes('hidden');
    });
    for (const edge of edges) {
      const path = edge.querySelector('.react-flow__edge-interaction, .react-flow__edge-path');
      if (!path || typeof path.getTotalLength !== 'function' || !path.ownerSVGElement) continue;
      const length = path.getTotalLength();
      if (!Number.isFinite(length) || length <= 0) continue;
      const localPoint = path.getPointAtLength(length / 2);
      const screenMatrix = path.getScreenCTM();
      if (!screenMatrix) continue;
      const svgPoint = path.ownerSVGElement.createSVGPoint();
      svgPoint.x = localPoint.x;
      svgPoint.y = localPoint.y;
      const screenPoint = svgPoint.matrixTransform(screenMatrix);
      return {
        id: edge.getAttribute('data-id'),
        count: edges.length,
        x: screenPoint.x,
        y: screenPoint.y,
      };
    }
    return null;
  });
  assert(target?.id, `${label} could not find a clickable expanded preview edge`);

  await page.mouse.click(target.x, target.y);
  await page.waitForFunction(
    (edgeID) => {
      const edge = [...document.querySelectorAll('.react-flow__edge[data-id]')].find(
        (el) => el.getAttribute('data-id') === edgeID,
      );
      return Boolean(edge?.classList.contains('selected') || edge?.getAttribute('aria-selected') === 'true');
    },
    { timeout: 5000 },
    target.id,
  );
  await page.keyboard.press('Backspace');
  await new Promise((resolve) => setTimeout(resolve, 500));

  const after = await page.evaluate((edgeID) => {
    const edges = [...document.querySelectorAll('.react-flow__edge[data-id^="preview-"]')].filter((edge) => {
      const id = edge.getAttribute('data-id') || '';
      return !id.includes('hidden');
    });
    return {
      count: edges.length,
      stillPresent: edges.some((edge) => edge.getAttribute('data-id') === edgeID),
    };
  }, target.id);
  assert(after.stillPresent, `${label} deleting selected preview edge removed ${target.id}`);
  assert(after.count === target.count, `${label} preview edge count changed after delete: ${after.count} vs ${target.count}`);
}

async function dragFirstPreviewNode(page, label) {
  const before = await page.evaluate((selector) => {
    const node = [...document.querySelectorAll(selector)].find((el) => el.textContent?.includes('Compiled preview'));
    if (!node) return null;
    const rect = node.getBoundingClientRect();
    return {
      id: node.getAttribute('data-node-id'),
      left: rect.left,
      top: rect.top,
      centerX: rect.left + rect.width / 2,
      centerY: rect.top + rect.height / 2,
    };
  }, previewNodeSelector);
  assert(before?.id, `${label} could not find a draggable preview node`);

  await page.mouse.move(before.centerX, before.centerY);
  await page.mouse.down();
  await page.mouse.move(before.centerX + 180, before.centerY + 80, { steps: 18 });
  await page.mouse.up();

  await page.waitForFunction(
    (selector, id, previousLeft, previousTop) => {
      const node = [...document.querySelectorAll(selector)].find((el) => el.getAttribute('data-node-id') === id);
      if (!node) return false;
      const rect = node.getBoundingClientRect();
      return Math.abs(rect.left - previousLeft) > 20 || Math.abs(rect.top - previousTop) > 20;
    },
    {},
    previewNodeSelector,
    before.id,
    before.left,
    before.top,
  );

  await assertFrameContainsPreviewNodes(page, `${label} after drag`);
}

async function dragPreviewFrame(page, label) {
  const before = await page.evaluate((frameSelectorArg, nodeSelectorArg) => {
    const frame = document.querySelector(frameSelectorArg);
    const previewNode = [...document.querySelectorAll(nodeSelectorArg)].find((el) =>
      el.textContent?.includes('Compiled preview'),
    );
    if (!frame || !previewNode) return null;
    const frameRect = frame.getBoundingClientRect();
    const previewRect = previewNode.getBoundingClientRect();
    return {
      frameId: frame.getAttribute('data-node-id'),
      frameLeft: frameRect.left,
      frameTop: frameRect.top,
      dragX: frameRect.left + 32,
      dragY: frameRect.top + 24,
      previewId: previewNode.getAttribute('data-node-id'),
      previewLeft: previewRect.left,
      previewTop: previewRect.top,
    };
  }, frameSelector, previewNodeSelector);
  assert(before?.frameId, `${label} could not find a draggable preview frame`);
  assert(before?.previewId, `${label} could not find a preview node before frame drag`);

  await page.mouse.move(before.dragX, before.dragY);
  await page.mouse.down();
  await page.mouse.move(before.dragX + 140, before.dragY + 70, { steps: 18 });
  await page.mouse.up();

  await page.waitForFunction(
    (frameSelectorArg, frameID, previousLeft, previousTop) => {
      const frame = [...document.querySelectorAll(frameSelectorArg)].find(
        (el) => el.getAttribute('data-node-id') === frameID,
      );
      if (!frame) return false;
      const rect = frame.getBoundingClientRect();
      return Math.abs(rect.left - previousLeft) > 20 || Math.abs(rect.top - previousTop) > 20;
    },
    {},
    frameSelector,
    before.frameId,
    before.frameLeft,
    before.frameTop,
  );

  const after = await page.evaluate((nodeSelectorArg, previewID) => {
    const previewNode = [...document.querySelectorAll(nodeSelectorArg)].find(
      (el) => el.getAttribute('data-node-id') === previewID,
    );
    if (!previewNode) return null;
    const rect = previewNode.getBoundingClientRect();
    return { left: rect.left, top: rect.top };
  }, previewNodeSelector, before.previewId);
  assert(after, `${label} preview node disappeared after frame drag`);
  assert(
    Math.abs(after.left - before.previewLeft) > 20 || Math.abs(after.top - before.previewTop) > 20,
    `${label} dragging the preview frame did not move preview internals`,
  );
  await assertFrameContainsPreviewNodes(page, `${label} after frame drag`);
}

async function main() {
  await fetchJSON(`${backendURL}/health`);
  const seed = await fetchJSON(`${backendURL}/api/workflows/${workflowID}`);
  const originalSeedJSON = JSON.stringify(seed);
  const collapsedAggregations = aggregationNodes(seed);
  assert(collapsedAggregations.length === 1, `expected one collapsed aggregation node, got ${collapsedAggregations.length}`);
  assert(
    collapsedAggregations[0].data?.config?.aggregationWorkflowId === 'aggregation-synthesis',
    `collapsed aggregation source = ${collapsedAggregations[0].data?.config?.aggregationWorkflowId}`,
  );

  const browser = await puppeteer.launch(puppeteerLaunchOptions());
  const page = await browser.newPage();
  await page.setViewport({ width: 1600, height: 1000, deviceScaleFactor: 1 });
  page.setDefaultTimeout(30000);
  page.on('console', (msg) => {
    if (msg.type() === 'error') console.error(`[browser] ${msg.text()}`);
  });

  try {
    await page.goto(`${frontendURL}/workflow/${workflowID}`, { waitUntil: 'networkidle0' });
    await page.waitForSelector('[data-testid="workflow-node-aggregation"]');
    const aggregationNodeText = await page.$eval('[data-testid="workflow-node-aggregation"]', (node) => node.textContent || '');
    assert(
      aggregationNodeText.includes('aggregation-synthesis'),
      'builder did not render the L0 aggregation source ID',
    );

    await page.click('[data-testid="workflow-node-aggregation"]');
    await page.waitForSelector('[data-testid="aggregation-expand-button"]:not([disabled])');
    await page.click('[data-testid="aggregation-expand-button"]');
    await page.waitForSelector(frameSelector);
    await page.waitForSelector('[data-testid="workflow-node-operation"]');
    await page.waitForFunction(() => document.body.innerText.includes('Compiled preview'));
    await assertExpandedPreviewVisible(page, workflowID);

    const afterExpand = await fetchJSON(`${backendURL}/api/workflows/${workflowID}`);
    assert(JSON.stringify(afterExpand) === originalSeedJSON, 'expanding aggregation preview mutated the saved seed workflow');

    await page.click('[data-testid="workflow-node-aggregation"]');
    await page.waitForSelector('[data-testid="aggregation-fork-button"]:not([disabled])');
    await page.click('[data-testid="aggregation-fork-button"]');
    await page.waitForFunction(
      () =>
        !document.querySelector('[data-testid="aggregation-frame-node"]') &&
        document.querySelectorAll('[data-testid="workflow-node-operation"]').length > 0 &&
        !document.body.innerText.includes('Compiled preview'),
    );

    const peerWorkflowID = 'reasoning-peer-score-pick-cheap';
    const { seedJSON: peerSeedJSON, group: peerGroup } = await compilePreviewForWorkflow(peerWorkflowID);
    assertPreviewCounts(peerWorkflowID, peerGroup, {
      llmJobs: 6,
      topLevelLLMJobs: 6,
      conditionalLLMJobs: 0,
    });

    await expandAggregationPreview(page, peerWorkflowID, 6);
    await page.waitForFunction(
      (selector) =>
        [...document.querySelectorAll(selector)].filter(
          (el) => el.getAttribute('data-testid') === 'workflow-node-agent' && el.textContent?.includes('review agent'),
        ).length === 6,
      {},
      previewNodeSelector,
    );

    const previewCounts = await page.evaluate((selector) => {
      const previewNodes = [...document.querySelectorAll(selector)].filter((el) =>
        el.textContent?.includes('Compiled preview'),
      );
      const reviewNodes = previewNodes.filter(
        (el) => el.getAttribute('data-testid') === 'workflow-node-agent' && el.textContent?.includes('review agent'),
      );
      const reviewPairs = reviewNodes.map((el) => {
        const id = el.getAttribute('data-node-id') || '';
        const pair = id.split('--review-agent-')[1] || '';
        const [reviewer, candidate] = pair.split('-agent-');
        return { id, reviewer, candidate };
      });
      return {
        frames: document.querySelectorAll('[data-testid="aggregation-frame-node"]').length,
        previewNodes: previewNodes.length,
        reviewNodes: reviewNodes.length,
        reviewPairs,
        body: document.body.innerText,
      };
    }, previewNodeSelector);
    assert(previewCounts.frames === 1, `expected one aggregation preview frame, got ${previewCounts.frames}`);
    assert(previewCounts.previewNodes >= 6, `expected at least 6 compiled preview nodes, got ${previewCounts.previewNodes}`);
    assert(previewCounts.reviewNodes === 6, `expected 6 peer review agent nodes, got ${previewCounts.reviewNodes}`);
    assert(previewCounts.body.includes('Compiled preview'), 'expanded peer matrix did not render compiled preview labels');
    for (const pair of previewCounts.reviewPairs) {
      assert(pair.reviewer && pair.candidate, `could not parse peer review node id: ${pair.id}`);
      assert(
        pair.reviewer !== pair.candidate,
        `expanded peer matrix rendered self-review: ${pair.reviewer} -> ${pair.candidate}`,
      );
    }

    await assertFrameContainsPreviewNodes(page, peerWorkflowID, 6);
    await assertExpandedPreviewVisible(page, peerWorkflowID);
    await assertPreviewEdgeDeleteIgnored(page, peerWorkflowID);
    await dragPreviewFrame(page, peerWorkflowID);
    await dragFirstPreviewNode(page, peerWorkflowID);

    const afterPeerExpand = await fetchJSON(`${backendURL}/api/workflows/${peerWorkflowID}`);
    assert(
      JSON.stringify(afterPeerExpand) === peerSeedJSON,
      'expanding or dragging peer matrix preview mutated saved seed workflow',
    );

    await page.click('[data-testid="workflow-node-aggregation"]');
    await page.waitForSelector('[data-testid="aggregation-collapse-button"]:not([disabled])');
    await page.click('[data-testid="aggregation-collapse-button"]');
    await page.waitForFunction(() => !document.querySelector('[data-testid="aggregation-frame-node"]'));

    const afterPeerCollapse = await fetchJSON(`${backendURL}/api/workflows/${peerWorkflowID}`);
    assert(
      JSON.stringify(afterPeerCollapse) === peerSeedJSON,
      'collapsing peer matrix preview mutated saved seed workflow',
    );

    const judgeWorkflowID = 'reasoning-judge-pick-cheap';
    const { seedJSON: judgeSeedJSON, group: judgeGroup } = await compilePreviewForWorkflow(judgeWorkflowID);
    assertPreviewCounts(judgeWorkflowID, judgeGroup, {
      llmJobs: 2,
      topLevelLLMJobs: 1,
      conditionalLLMJobs: 1,
    });

    await expandAggregationPreview(page, judgeWorkflowID, 2);

    const judgePreviewCounts = await page.evaluate((selector) => {
      const previewNodes = [...document.querySelectorAll(selector)].filter((el) =>
        el.textContent?.includes('Compiled preview'),
      );
      return {
        frames: document.querySelectorAll('[data-testid="aggregation-frame-node"]').length,
        conditionalNodes: previewNodes.filter((el) => el.getAttribute('data-testid') === 'workflow-node-conditional').length,
        repairAgents: previewNodes.filter(
          (el) => el.getAttribute('data-testid') === 'workflow-node-agent' && el.textContent?.includes('repair selection call'),
        ).length,
        body: document.body.innerText,
      };
    }, previewNodeSelector);
    assert(judgePreviewCounts.frames === 1, `expected one judge preview frame, got ${judgePreviewCounts.frames}`);
    assert(judgePreviewCounts.body.includes('Compiled preview'), 'expanded judge did not render compiled preview labels');
    assert(
      judgePreviewCounts.conditionalNodes >= 1,
      `expected at least one judge conditional preview node, got ${judgePreviewCounts.conditionalNodes}`,
    );
    assert(
      judgePreviewCounts.repairAgents === 1,
      `expected one judge conditional repair LLM node, got ${judgePreviewCounts.repairAgents}`,
    );
    await assertExpandedPreviewVisible(page, judgeWorkflowID);

    const afterJudgeExpand = await fetchJSON(`${backendURL}/api/workflows/${judgeWorkflowID}`);
    assert(JSON.stringify(afterJudgeExpand) === judgeSeedJSON, 'expanding judge preview mutated saved seed workflow');

    await page.click('[data-testid="workflow-node-aggregation"]');
    await page.waitForSelector('[data-testid="aggregation-collapse-button"]:not([disabled])');
    await page.click('[data-testid="aggregation-collapse-button"]');
    await page.waitForFunction(() => !document.querySelector('[data-testid="aggregation-frame-node"]'));

    const afterJudgeCollapse = await fetchJSON(`${backendURL}/api/workflows/${judgeWorkflowID}`);
    assert(JSON.stringify(afterJudgeCollapse) === judgeSeedJSON, 'collapsing judge preview mutated saved seed workflow');

    const dynamicScoringWorkflowID = 'reasoning-judge-score-pick';
    const {
      seedJSON: dynamicScoringSeedJSON,
      group: dynamicScoringGroup,
    } = await compilePreviewForWorkflow(dynamicScoringWorkflowID);
    assertPreviewCounts(dynamicScoringWorkflowID, dynamicScoringGroup, {
      llmJobs: 4,
      topLevelLLMJobs: 4,
      conditionalLLMJobs: 0,
    });
    await expandAggregationPreview(page, dynamicScoringWorkflowID, 4);
    await assertFrameContainsPreviewNodes(page, dynamicScoringWorkflowID, 4);
    await assertExpandedPreviewVisible(page, dynamicScoringWorkflowID);
    const afterDynamicScoringExpand = await fetchJSON(`${backendURL}/api/workflows/${dynamicScoringWorkflowID}`);
    assert(
      JSON.stringify(afterDynamicScoringExpand) === dynamicScoringSeedJSON,
      'expanding dynamic scoring preview mutated saved seed workflow',
    );

    const dynamicPeerWorkflowID = 'reasoning-peer-score-pick';
    const { seedJSON: dynamicPeerSeedJSON, group: dynamicPeerGroup } = await compilePreviewForWorkflow(dynamicPeerWorkflowID);
    assertPreviewCounts(dynamicPeerWorkflowID, dynamicPeerGroup, {
      llmJobs: 7,
      topLevelLLMJobs: 7,
      conditionalLLMJobs: 0,
    });
    await expandAggregationPreview(page, dynamicPeerWorkflowID, 7);
    await assertFrameContainsPreviewNodes(page, dynamicPeerWorkflowID, 7);
    await assertExpandedPreviewVisible(page, dynamicPeerWorkflowID);
    const afterDynamicPeerExpand = await fetchJSON(`${backendURL}/api/workflows/${dynamicPeerWorkflowID}`);
    assert(
      JSON.stringify(afterDynamicPeerExpand) === dynamicPeerSeedJSON,
      'expanding dynamic peer matrix preview mutated saved seed workflow',
    );
  } finally {
    await browser.close();
  }

  const submit = await fetchJSON(`${backendURL}/api/workflows/submit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      workflow_file: seed,
      input_values: {
        user_prompt: 'Which option is correct?\nA. Alpha\nB. Beta\nC. Gamma\nD. Delta',
      },
      idempotency_key: `builder-e2e-${Date.now()}`,
      force_new_run: true,
    }),
  });
  assert(submit.job_id, `submit response missing job_id: ${JSON.stringify(submit)}`);
  const job = await waitForJob(submit.job_id);
  assert(job.status === 'completed', `job status = ${job.status}`);
  assert(hasCompiledSourceNode(job.nodes), 'job nodes missing compiled aggregation internals');

  const adminDetail = await fetchJSON(`${backendURL}/api/admin/jobs/${submit.job_id}`);
  const grouped = (adminDetail.NodesWithMetrics || []).some((node) => node.parent_node_id || node.ParentNodeID);
  assert(grouped, 'admin job detail did not return grouped compiled aggregation internals');

  const roundTripID = `e2e-builder-collapsed-${Date.now()}`;
  const clone = { ...seed, id: roundTripID, name: `E2E Builder Collapsed ${Date.now()}` };
  await fetchJSON(`${backendURL}/api/workflows`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(clone),
  });
  const roundTrip = await fetchJSON(`${backendURL}/api/workflows/${roundTripID}`);
  assert(
    stableJSON(aggregationNodes(roundTrip).map((node) => node.data?.config?.aggregationWorkflowId)) ===
      stableJSON(['aggregation-synthesis']),
    'collapsed aggregation source did not round-trip through workflow API',
  );

  console.log(`builder aggregation e2e passed: ${frontendURL} / ${backendURL}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
