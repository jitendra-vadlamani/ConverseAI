export interface LLMConfig {
  id?: string;
  provider: 'ollama' | 'openai' | 'claude' | 'custom';
  name: string;
  model_name: string;
  base_url?: string;
  api_key?: string;
  description?: string;
  context_window?: number;
}

export interface LLMInfo {
  config: LLMConfig;
  status: 'Online' | 'Offline' | 'Missing';
  is_system: boolean;
}

export const listLLMsApi = async (): Promise<LLMInfo[]> => {
  const response = await fetch('/api/llms');
  if (!response.ok) {
    const errorData = await response.text();
    throw new Error(errorData || 'Failed to list models');
  }
  return response.json();
};

export const addLLMApi = async (config: LLMConfig): Promise<LLMConfig> => {
  const response = await fetch('/api/llms/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });

  if (!response.ok) {
    const errorData = await response.text();
    throw new Error(errorData || 'Failed to add model');
  }
  return response.json();
};

export const deleteLLMApi = async (id: string): Promise<void> => {
  const response = await fetch(`/api/llms/delete?id=${id}`, {
    method: 'DELETE',
  });

  if (!response.ok) {
    const errorData = await response.text();
    throw new Error(errorData || 'Failed to delete model');
  }
};
