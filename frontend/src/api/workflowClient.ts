import axios from 'axios';
import type { CompilePreviewResponse, Workflow } from '../types/workflow';
import type { WorkflowFileFormat } from '../types/workflowFile';

const API_BASE_URL = '/api';

export interface WorkflowExecutionResult {
  success: boolean;
  job_id: string;
  workflow_id: string;
  result?: {
    workflow_id: string;
    success: boolean;
    final_output: string;
    node_results: Array<{
      node_id: string;
      output: string;
      tokens_input: number;
      tokens_output: number;
      cost: number;
      latency: number;
      error?: string;
    }>;
    total_tokens: number;
    total_cost: number;
    total_latency: number;
    error?: string;
  };
  error?: string;
}

export interface JobWorkflowSnapshotResponse {
  job_id: string;
  workflow_id: string;
  workflow: Workflow;
  source: 'request_data' | 'dag_snapshot' | string;
  saved_workflow_exists: boolean;
}

export const workflowClient = {
  async listWorkflows() {
    const response = await axios.get(`${API_BASE_URL}/workflows`);
    return response.data.workflows;
  },

  async getWorkflow(id: string) {
    const response = await axios.get(`${API_BASE_URL}/workflows/${id}`);
    return response.data;
  },

  async getJobWorkflow(jobId: string): Promise<JobWorkflowSnapshotResponse> {
    const response = await axios.get<JobWorkflowSnapshotResponse>(`${API_BASE_URL}/jobs/${jobId}/workflow`);
    return response.data;
  },

  async createWorkflow(workflow: Workflow) {
    const response = await axios.post(`${API_BASE_URL}/workflows`, workflow);
    return response.data;
  },

  async updateWorkflow(id: string, workflow: Workflow | Partial<WorkflowFileFormat>) {
    const response = await axios.put(`${API_BASE_URL}/workflows/${id}`, workflow);
    return response.data;
  },

  async deleteWorkflow(id: string) {
    const response = await axios.delete(`${API_BASE_URL}/workflows/${id}`);
    return response.data;
  },

  async executeWorkflow(workflow: Workflow): Promise<WorkflowExecutionResult> {
    const response = await axios.post(`${API_BASE_URL}/workflows/execute`, workflow);
    return response.data;
  },

  async compilePreview(payload: {
    workflow?: Workflow;
    workflow_file?: Partial<WorkflowFileFormat>;
    input_values?: Record<string, unknown>;
  }): Promise<CompilePreviewResponse> {
    const response = await axios.post<CompilePreviewResponse>(`${API_BASE_URL}/workflows/compile-preview`, payload);
    return response.data;
  },

  async saveWorkflow(workflowFile: Partial<WorkflowFileFormat>) {
    // Check if workflow already exists
    if (workflowFile.id) {
      try {
        await axios.get(`${API_BASE_URL}/workflows/${workflowFile.id}`);
        // Workflow exists, update it
        const response = await axios.put(`${API_BASE_URL}/workflows/${workflowFile.id}`, workflowFile);
        return response.data;
      } catch (_error) {
        // Workflow doesn't exist, create it
        const response = await axios.post(`${API_BASE_URL}/workflows`, workflowFile);
        return response.data;
      }
    } else {
      // No ID, create new
      const response = await axios.post(`${API_BASE_URL}/workflows`, workflowFile);
      return response.data;
    }
  },
};
