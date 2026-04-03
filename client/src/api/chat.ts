

export interface Message {
  role: 'user' | 'assistant' | 'system';
  content: string;
  reasoning?: string;
  created_at?: string;
}

export interface Conversation {
  id: string;
  user_id: string;
  title: string;
  model_config_id?: string;
  model_name?: string;
  messages: Message[];
  created_at: string;
  updated_at: string;
}

export const listConversationsApi = async (): Promise<Conversation[]> => {
  const response = await fetch('/api/chat/conversations');
  if (!response.ok) throw new Error('Failed to list conversations');
  return response.json();
};

export const createConversationApi = async (title: string, modelConfigId?: string, modelName?: string): Promise<Conversation> => {
  const response = await fetch('/api/chat/conversations/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ 
      title, 
      model_config_id: modelConfigId,
      model_name: modelName 
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

export const streamCompletionApi = async (
  conversationId: string,
  content: string,
  onThought: (thought: string) => void,
  onChunk: (chunk: string) => void,
  onEnd: () => void,
  onError: (err: string) => void,
  signal?: AbortSignal
) => {
  try {
    const response = await fetch('/api/chat/completions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ conversation_id: conversationId, content }),
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
