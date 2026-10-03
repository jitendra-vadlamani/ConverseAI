export interface Message {
  id?: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  reasoning?: string;
  model_name: string;
  attachments?: string[];
  run_id?: string;
  token_count?: number;
  is_summarized?: boolean;
  created_at?: string;
}

export interface Conversation {
  id: string;
  user_id: string;
  title: string;
  messages: Message[];
  total_tokens?: number;
  summary?: string;
  summary_token_count?: number;
  active_run_id?: string;
  created_at: string;
  updated_at: string;
}

export interface ConversationSummary {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface ModelInfo {
  name: string;
  model_name: string;
  description?: string;
  context_window: number;
  capabilities?: string[];
}

export interface Evidence {
  content: string;
  source: string;
  url?: string;
  relevance_score: number;
  authority_score?: number;
  freshness_score?: number;
  final_score: number;
  is_conflicting?: boolean;
  conflict_reason?: string;
}

export interface ConversationEvent {
  id: string;
  conversation_id: string;
  user_id: string;
  run_id?: string;
  type: string;
  payload: Record<string, unknown> | null;
  timestamp: string;
}

async function check(response: Response, fallback: string): Promise<Response> {
  if (!response.ok) {
    const text = (await response.text()).trim();
    throw new Error(text || fallback);
  }
  return response;
}

const jsonHeaders = { 'Content-Type': 'application/json' };

export const listConversationsApi = async (): Promise<ConversationSummary[]> => {
  const response = await check(await fetch('/api/chat/conversations'), 'Failed to list conversations');
  return response.json();
};

export const listModelsApi = async (): Promise<ModelInfo[]> => {
  const response = await check(await fetch('/api/models'), 'Failed to list models');
  return response.json();
};

export const createConversationApi = async (title: string): Promise<Conversation> => {
  const response = await check(
    await fetch('/api/chat/conversations/create', { method: 'POST', headers: jsonHeaders, body: JSON.stringify({ title }) }),
    'Failed to create conversation',
  );
  return response.json();
};

export const getConversationApi = async (id: string): Promise<Conversation> => {
  const response = await check(await fetch(`/api/chat/conversations/get?id=${encodeURIComponent(id)}`), 'Failed to get conversation');
  return response.json();
};

export const deleteConversationApi = async (id: string): Promise<void> => {
  await check(await fetch(`/api/chat/conversations/delete?id=${encodeURIComponent(id)}`, { method: 'DELETE' }), 'Failed to delete conversation');
};

export const getEventsApi = async (id: string): Promise<ConversationEvent[]> => {
  const response = await check(await fetch(`/api/chat/conversations/events?id=${encodeURIComponent(id)}`), 'Failed to get events');
  return (await response.json()) || [];
};

export const updateConversationTitleApi = async (id: string, title: string): Promise<void> => {
  await check(
    await fetch('/api/chat/conversations/title', { method: 'PATCH', headers: jsonHeaders, body: JSON.stringify({ id, title }) }),
    'Failed to update conversation title',
  );
};

export const listConversationFilesApi = async (id: string): Promise<string[]> => {
  const response = await fetch(`/api/chat/conversations/files?id=${encodeURIComponent(id)}`);
  if (!response.ok) return [];
  return (await response.json()) || [];
};

export const fileDownloadUrl = (fileID: string): string => `/api/chat/files/download?fileID=${encodeURIComponent(fileID)}`;

export const deleteConversationFileApi = async (id: string, fileID: string): Promise<void> => {
  await check(
    await fetch(`/api/chat/conversations/files?id=${encodeURIComponent(id)}&fileID=${encodeURIComponent(fileID)}`, { method: 'DELETE' }),
    'Failed to delete file',
  );
};

export const cancelRunApi = async (runId: string): Promise<void> => {
  await fetch(`/api/chat/runs/cancel?id=${encodeURIComponent(runId)}`, { method: 'POST' });
};

export const submitFeedbackApi = async (conversationId: string, messageId: string, rating: 1 | -1, correction = ''): Promise<void> => {
  await check(
    await fetch('/api/chat/feedback', {
      method: 'POST',
      headers: jsonHeaders,
      body: JSON.stringify({ conversation_id: conversationId, message_id: messageId, rating, correction }),
    }),
    'Failed to send feedback',
  );
};

export interface RunHandlers {
  onRun?: (runId: string) => void;
  onThought: (text: string) => void;
  // onAnswer receives the full answer text so far; it is replaced, not
  // appended, so a server-side reset (retry or resume) is handled for free.
  onAnswer: (text: string) => void;
  onStatus?: (text: string) => void;
  onError: (message: string) => void;
  onDone: (status: string) => void;
}

interface StreamPayload {
  type?: string;
  text?: string;
  seq?: number;
  status?: string;
  run_id?: string;
}

// readRunStream parses Server-Sent Events: frames are separated by a blank
// line and every data line is JSON, so newlines inside the answer are safe.
async function readRunStream(response: Response, h: RunHandlers): Promise<void> {
  const reader = response.body?.getReader();
  if (!reader) throw new Error('Streaming is not supported by this browser');
  const decoder = new TextDecoder();
  let buffer = '';
  let answer = '';
  let finished = false;

  const handleFrame = (frame: string) => {
    let event = 'message';
    const data: string[] = [];
    for (const line of frame.split('\n')) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      else if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
    }
    if (data.length === 0) return;
    let payload: StreamPayload;
    try {
      payload = JSON.parse(data.join('\n'));
    } catch {
      return;
    }
    switch (event) {
      case 'run':
        if (payload.run_id) h.onRun?.(payload.run_id);
        break;
      case 'delta':
        answer += payload.text ?? '';
        h.onAnswer(answer);
        break;
      case 'reset':
        answer = payload.text ?? '';
        h.onAnswer(answer);
        break;
      case 'thought':
        h.onThought(payload.text ?? '');
        break;
      case 'status':
        h.onStatus?.(payload.text ?? '');
        break;
      case 'error':
        h.onError(payload.text ?? 'The answer failed');
        break;
      case 'done':
        finished = true;
        h.onDone(payload.status ?? 'succeeded');
        break;
    }
  };

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
    let idx;
    while ((idx = buffer.indexOf('\n\n')) >= 0) {
      handleFrame(buffer.slice(0, idx));
      buffer = buffer.slice(idx + 2);
      if (finished) return;
    }
  }
  if (!finished) throw new Error('Connection closed before the answer finished');
}

export const streamCompletionApi = async (
  conversationId: string,
  modelName: string,
  content: string,
  handlers: RunHandlers,
  files: File[] = [],
  signal?: AbortSignal,
): Promise<void> => {
  let init: RequestInit;
  if (files.length > 0) {
    const formData = new FormData();
    formData.append('conversation_id', conversationId);
    formData.append('model_name', modelName);
    formData.append('content', content);
    files.forEach(f => formData.append('files', f));
    init = { method: 'POST', body: formData, signal };
  } else {
    init = {
      method: 'POST',
      headers: jsonHeaders,
      body: JSON.stringify({ conversation_id: conversationId, model_name: modelName, content }),
      signal,
    };
  }
  const response = await check(await fetch('/api/chat/completions', init), 'Failed to send message');
  await readRunStream(response, handlers);
};

// attachRunApi re-attaches to an answer that is still being generated, for
// example after a page reload.
export const attachRunApi = async (runId: string, handlers: RunHandlers, signal?: AbortSignal): Promise<void> => {
  const response = await check(await fetch(`/api/chat/runs/stream?id=${encodeURIComponent(runId)}`, { signal }), 'Failed to attach to the answer');
  await readRunStream(response, handlers);
};
