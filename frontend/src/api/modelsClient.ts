export interface Model {
  id: string;
  name: string;
  provider: string;
  context_length: number;
  input_cost_per_token: number;
  output_cost_per_token: number;
  max_tokens: number;
  available: boolean;
  supported_parameters?: string[];
  input_modalities?: string[];
  output_modalities?: string[];
}

export interface ModelsResponse {
  models: Model[];
}

class ModelsClient {
  private baseURL: string;

  constructor(baseURL = '') {
    this.baseURL = baseURL.replace(/\/+$/, '');
  }

  async getModels(provider?: string): Promise<Model[]> {
    try {
      const url = provider ? `${this.baseURL}/api/models?provider=${provider}` : `${this.baseURL}/api/models`;

      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to fetch models: ${response.statusText}`);
      }

      const data: ModelsResponse = await response.json();
      return data.models || [];
    } catch (error) {
      console.error('Error fetching models:', error);
      return [];
    }
  }
}

export const modelsClient = new ModelsClient();
