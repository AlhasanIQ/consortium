import { describe, expect, it } from 'bun:test';
import { NEW_WORKFLOW_PATH, WORKFLOW_BUILDER_ROUTE_PATHS } from './workflowRoutes';

describe('workflow routes', () => {
  it('uses /workflow as the canonical new workflow builder route', () => {
    expect(NEW_WORKFLOW_PATH).toBe('/workflow');
    expect(WORKFLOW_BUILDER_ROUTE_PATHS).toContain('/workflow');
    expect(WORKFLOW_BUILDER_ROUTE_PATHS).toContain('/builder');
  });
});
