import axios from 'axios';
import { setBackendOnline } from '../hooks/useBackendStatus';

// Axios interceptor — covers adminClient, workflowClient
axios.interceptors.response.use(
  (response) => {
    setBackendOnline(true);
    return response;
  },
  (error) => {
    if (axios.isAxiosError(error) && error.response?.data?.proxy_error) {
      setBackendOnline(false);
      error.message = 'Backend server is offline';
    }
    return Promise.reject(error);
  },
);

// Global fetch wrapper — covers raw fetch() calls (ensemble, workflow execution, etc.)
const _origFetch = window.fetch;
window.fetch = Object.assign(async (...args: Parameters<typeof fetch>): Promise<Response> => {
  const response = await _origFetch.apply(window, args);
  const url = typeof args[0] === 'string' ? args[0] : args[0] instanceof Request ? args[0].url : '';
  if (url.startsWith('/api') || url.startsWith('/health')) {
    if (response.ok) {
      setBackendOnline(true);
    } else if (response.status === 502 && response.headers.get('x-proxy-error')) {
      setBackendOnline(false);
    }
  }
  return response;
}, _origFetch);
