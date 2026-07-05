export const NEW_WORKFLOW_PATH = '/workflow';
export const LEGACY_WORKFLOW_BUILDER_PATH = '/builder';
export const WORKFLOW_BUILDER_ROUTE_PATHS = [NEW_WORKFLOW_PATH, LEGACY_WORKFLOW_BUILDER_PATH] as const;

export function workflowDetailPath(workflowId: string): string {
  return `/workflow/${workflowId}`;
}
