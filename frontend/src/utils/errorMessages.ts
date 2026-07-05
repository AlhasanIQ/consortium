// Provider error code display mapping
// Codes match backend pkg/providers/errors.go and pkg/api/errors.go

export interface ProviderErrorInfo {
  label: string;
  action: string;
}

export const PROVIDER_ERROR_INFO: Record<string, ProviderErrorInfo> = {
  INSUFFICIENT_CREDITS: {
    label: 'Insufficient Credits',
    action: 'Add credits at openrouter.ai/credits',
  },
  RATE_LIMITED: {
    label: 'Rate Limited',
    action: 'Wait a moment and retry',
  },
  UPSTREAM_ERROR: {
    label: 'Provider Unavailable',
    action: 'Try again or use a different model',
  },
  UPSTREAM_TIMEOUT: {
    label: 'Provider Timeout',
    action: 'Try again',
  },
  AUTH_ERROR: {
    label: 'Authentication Error',
    action: 'Check your OpenRouter API key',
  },
  MODEL_NOT_FOUND: {
    label: 'Model Not Found',
    action: 'Select a different model',
  },
  BAD_REQUEST: {
    label: 'Bad Request',
    action: 'Check your workflow configuration',
  },
  CONTENT_FILTERED: {
    label: 'Content Filtered',
    action: 'Modify your prompt and try again',
  },
  COST_LIMIT_EXCEEDED: {
    label: 'Cost Limit Exceeded',
    action: 'Increase cost limit or reduce workflow scope',
  },
  TOKEN_LIMIT_EXCEEDED: {
    label: 'Token Limit Exceeded',
    action: 'Reduce prompt length or max tokens',
  },
};

export function getErrorInfo(code: string | undefined): ProviderErrorInfo | null {
  if (!code) return null;
  return PROVIDER_ERROR_INFO[code] ?? null;
}
