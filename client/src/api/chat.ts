

export interface Message {
  role: 'user' | 'assistant' | 'system';
  content: string;
  reasoning?: string;
  model_name: string;
  token_count?: number;
  is_summarized?: boolean;
  created_at?: string;
  attachments?: string[];
}

export interface Conversation {
  id: string;
  user_id: string;
  title: string;
  messages: Message[];
  total_tokens?: number;
  summary?: string;
  summary_token_count?: number;
  created_at: string;
  updated_at: string;
}

export interface Evidence {
  content: string;
  source: string;
  url?: string;
  relevance_score: number;
  authority_score?: number;
  freshness_score?: number;
  final_score: number;
}

export interface ConversationEvent {
  id: string;
  conversation_id: string;
  user_id: string;
  type: string;
  payload: any;
  timestamp: string;
}

export const listConversationsApi = async (): Promise<Conversation[]> => {
  const response = await fetch('/api/chat/conversations');
  if (!response.ok) throw new Error('Failed to list conversations');
  return response.json();
};

export const listModelsApi = async (): Promise<any[]> => {
  const response = await fetch('/api/models');
  if (!response.ok) throw new Error('Failed to list models');
  return response.json();
};

export const createConversationApi = async (title: string): Promise<Conversation> => {
  const response = await fetch('/api/chat/conversations/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      title
    }),
  });
  if (!response.ok) throw new Error('Failed to create conversation');
  return response.json();
};

export const getConversationApi = async (id: string): Promise<Conversation> => {
  const response = await fetch(`/api/chat/conversations/get?id=${id}`);
  if (!response.ok) throw new Error('Failed to get conversation');
  return response.json();
};

export const deleteConversationApi = async (id: string): Promise<void> => {
  const response = await fetch(`/api/chat/conversations/delete?id=${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) throw new Error('Failed to delete conversation');
};

export const getEventsApi = async (id: string): Promise<ConversationEvent[]> => {
  const response = await fetch(`/api/chat/conversations/events?id=${id}`);
  if (!response.ok) throw new Error('Failed to get events');
  return response.json();
};

export const updateConversationTitleApi = async (id: string, title: string) => {
  const response = await fetch('/api/chat/conversations/title', {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${localStorage.getItem('token')}`,
    },
    body: JSON.stringify({ id, title }),
  });

  if (!response.ok) {
    const errorData = await response.json();
    throw new Error(errorData.message || 'Failed to update conversation title');
  }
};

export const listConversationFilesApi = async (id: string) => {
  const response = await fetch(`/api/chat/conversations/files?id=${id}`, {
    headers: {
      'Authorization': `Bearer ${localStorage.getItem('token')}`,
    },
  });
  if (!response.ok) return [];
  return response.json();
};

export const getPresignedUrlApi = async (fileID: string) => {
  const response = await fetch(`/api/chat/files/presign?fileID=${encodeURIComponent(fileID)}`, {
    headers: {
      'Authorization': `Bearer ${localStorage.getItem('token')}`,
    },
  });
  if (!response.ok) throw new Error('Failed to get download URL');
  const data = await response.json();
  return data.url;
};

export const deleteConversationFileApi = async (id: string, fileID: string) => {
  const response = await fetch(`/api/chat/conversations/files?id=${id}&fileID=${encodeURIComponent(fileID)}`, {
    method: 'DELETE',
    headers: {
      'Authorization': `Bearer ${localStorage.getItem('token')}`,
    },
  });
  if (!response.ok) throw new Error('Failed to delete file');
};

export const streamCompletionApi = async (
  conversationId: string,
  modelName: string,
  content: string,
  onThought: (thought: string) => void,
  onChunk: (chunk: string) => void,
  onEnd: () => void,
  onError: (err: string) => void,
  files?: File[],
  signal?: AbortSignal
) => {
  try {
    let body: any;
    let headers: Record<string, string> = {};

    if (files && files.length > 0) {
      const formData = new FormData();
      formData.append('conversation_id', conversationId);
      formData.append('model_name', modelName);
      formData.append('content', content);
      files.forEach(f => formData.append('files', f));
      body = formData;
      // Fetch will automatically set multipart/form-data with the correct boundary
    } else {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify({ conversation_id: conversationId, model_name: modelName, content });
    }

    const response = await fetch('/api/chat/completions', {
      method: 'POST',
      headers,
      body,
      signal
    });

    if (!response.ok) {
      throw new Error(await response.text() || 'Failed to start stream');
    }

    const reader = response.body?.getReader();
    if (!reader) throw new Error('Readable stream not supported');

    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';

      let currentEvent = 'message';
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) continue;

        if (trimmed.startsWith('event: ')) {
          currentEvent = trimmed.substring(7);
          continue;
        }

        if (trimmed.startsWith('data: ')) {
          const data = trimmed.substring(6);
          if (data === '[DONE]') {
            onEnd();
            return;
          }
          if (currentEvent === 'thought') {
            onThought(data);
          } else if (currentEvent === 'error') {
            onError(data);
            return; // Stop stream on error
          } else {
            onChunk(data);
          }
          // Reset event for next chunk unless explicit event tag
          if (currentEvent !== 'message') currentEvent = 'message';
        }
      }
    }
  } catch (err: any) {
    onError(err.message || 'Stream error');
  }
};
