import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  streamCompletionApi,
  attachRunApi,
  fileDownloadUrl,
  getConversationApi,
  listConversationFilesApi,
  submitFeedbackApi,
  type RunHandlers,
} from './chat';

// Builds a streamed Response whose body arrives in exactly these pieces, so
// frames and lines can be split at awkward places.
function streamResponse(pieces: string[], init: ResponseInit = { status: 200 }): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const p of pieces) controller.enqueue(encoder.encode(p));
      controller.close();
    },
  });
  return new Response(body, init);
}

function frame(event: string, data: object): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}

function recorder() {
  const calls = {
    run: [] as string[],
    answers: [] as string[],
    thoughts: [] as string[],
    statuses: [] as string[],
    errors: [] as string[],
    done: [] as string[],
  };
  const handlers: RunHandlers = {
    onRun: id => calls.run.push(id),
    onAnswer: t => calls.answers.push(t),
    onThought: t => calls.thoughts.push(t),
    onStatus: t => calls.statuses.push(t),
    onError: m => calls.errors.push(m),
    onDone: s => calls.done.push(s),
  };
  return { calls, handlers };
}

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

describe('run stream parsing', () => {
  it('assembles the answer from deltas and keeps newlines inside the text', async () => {
    const body =
      frame('run', { run_id: 'r1', conversation_id: 'c1' }) +
      frame('thought', { type: 'thought', text: 'thinking...' }) +
      frame('delta', { type: 'delta', seq: 1, text: 'Line one.\n\n' }) +
      frame('delta', { type: 'delta', seq: 2, text: 'data: not a frame\nLine two' }) +
      frame('done', { type: 'done', status: 'succeeded' });
    // Split into 7-byte pieces so frames, lines and JSON are all cut mid-way.
    const pieces = body.match(/[\s\S]{1,7}/g)!;
    fetchMock.mockResolvedValue(streamResponse(pieces));
    const { calls, handlers } = recorder();

    await streamCompletionApi('c1', 'gemma4:latest', 'hi', handlers);

    expect(calls.run).toEqual(['r1']);
    expect(calls.thoughts).toEqual(['thinking...']);
    expect(calls.answers.at(-1)).toBe('Line one.\n\ndata: not a frame\nLine two');
    expect(calls.done).toEqual(['succeeded']);
    expect(calls.errors).toEqual([]);
  });

  it('replaces the answer on reset (server retry or resume)', async () => {
    fetchMock.mockResolvedValue(streamResponse([
      frame('delta', { seq: 1, text: 'partial that will be withdrawn' }),
      frame('reset', { seq: 2, text: 'Fresh ' }),
      frame('delta', { seq: 3, text: 'start' }),
      frame('done', { status: 'succeeded' }),
    ]));
    const { calls, handlers } = recorder();
    await streamCompletionApi('c1', 'm', 'q', handlers);
    expect(calls.answers.at(-1)).toBe('Fresh start');
  });

  it('treats an empty reset as clearing the answer', async () => {
    fetchMock.mockResolvedValue(streamResponse([
      frame('delta', { seq: 1, text: 'old' }),
      frame('reset', { seq: 2 }),
      frame('done', { status: 'succeeded' }),
    ]));
    const { calls, handlers } = recorder();
    await streamCompletionApi('c1', 'm', 'q', handlers);
    expect(calls.answers.at(-1)).toBe('');
  });

  it('reports errors and the final status', async () => {
    fetchMock.mockResolvedValue(streamResponse([
      frame('status', { text: 'Waiting for other answers to finish...' }),
      frame('error', { text: 'the model server failed to answer' }),
      frame('done', { status: 'failed' }),
    ]));
    const { calls, handlers } = recorder();
    await streamCompletionApi('c1', 'm', 'q', handlers);
    expect(calls.statuses).toEqual(['Waiting for other answers to finish...']);
    expect(calls.errors).toEqual(['the model server failed to answer']);
    expect(calls.done).toEqual(['failed']);
  });

  it('handles CRLF line endings and ignores comments and malformed data', async () => {
    fetchMock.mockResolvedValue(streamResponse([
      ': ping\r\n\r\n',
      'event: delta\r\ndata: {not json}\r\n\r\n',
      'event: delta\r\ndata: {"seq":1,"text":"ok"}\r\n\r\n',
      'event: done\r\ndata: {"status":"succeeded"}\r\n\r\n',
    ]));
    const { calls, handlers } = recorder();
    await streamCompletionApi('c1', 'm', 'q', handlers);
    expect(calls.answers).toEqual(['ok']);
    expect(calls.done).toEqual(['succeeded']);
  });

  it('fails if the connection closes before done', async () => {
    fetchMock.mockResolvedValue(streamResponse([frame('delta', { seq: 1, text: 'half' })]));
    const { handlers } = recorder();
    await expect(streamCompletionApi('c1', 'm', 'q', handlers)).rejects.toThrow(/closed before the answer finished/);
  });

  it('stops reading at done even if more data follows', async () => {
    fetchMock.mockResolvedValue(streamResponse([
      frame('done', { status: 'succeeded' }) + frame('delta', { seq: 9, text: 'ignored' }),
    ]));
    const { calls, handlers } = recorder();
    await streamCompletionApi('c1', 'm', 'q', handlers);
    expect(calls.answers).toEqual([]);
  });
});

describe('requests', () => {
  const done = () => streamResponse([frame('done', { status: 'succeeded' })]);

  it('sends JSON when there are no files', async () => {
    fetchMock.mockResolvedValue(done());
    await streamCompletionApi('c1', 'gemma4:latest', 'hello', recorder().handlers);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/chat/completions');
    expect(init.method).toBe('POST');
    expect(init.headers).toEqual({ 'Content-Type': 'application/json' });
    expect(JSON.parse(init.body)).toEqual({ conversation_id: 'c1', model_name: 'gemma4:latest', content: 'hello' });
  });

  it('sends multipart form data with files and lets the browser set the boundary', async () => {
    fetchMock.mockResolvedValue(done());
    const file = new File(['text'], 'notes.txt', { type: 'text/plain' });
    await streamCompletionApi('c1', 'm', 'read this', recorder().handlers, [file]);
    const init = fetchMock.mock.calls[0][1];
    expect(init.headers).toBeUndefined();
    const form = init.body as FormData;
    expect(form.get('conversation_id')).toBe('c1');
    expect(form.get('content')).toBe('read this');
    expect((form.get('files') as File).name).toBe('notes.txt');
  });

  it('surfaces the server error message when the request is rejected', async () => {
    fetchMock.mockResolvedValue(new Response('this conversation is still answering the previous message\n', { status: 409 }));
    await expect(streamCompletionApi('c1', 'm', 'q', recorder().handlers)).rejects.toThrow(
      'this conversation is still answering the previous message',
    );
  });

  it('re-attaches to a run by id', async () => {
    fetchMock.mockResolvedValue(done());
    await attachRunApi('run/1', recorder().handlers);
    expect(fetchMock.mock.calls[0][0]).toBe('/api/chat/runs/stream?id=run%2F1');
  });

  it('encodes ids in URLs', async () => {
    expect(fileDownloadUrl('user-1/123-a b&c.pdf')).toBe('/api/chat/files/download?fileID=user-1%2F123-a%20b%26c.pdf');
    fetchMock.mockResolvedValue(new Response('{"id":"x","messages":[]}'));
    await getConversationApi('a&b');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/chat/conversations/get?id=a%26b');
  });

  it('returns an empty file list instead of failing', async () => {
    fetchMock.mockResolvedValue(new Response('Not found', { status: 404 }));
    expect(await listConversationFilesApi('c1')).toEqual([]);
  });

  it('posts feedback as JSON', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await submitFeedbackApi('c1', 'm1', -1, 'should say 42');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/chat/feedback');
    expect(JSON.parse(init.body)).toEqual({ conversation_id: 'c1', message_id: 'm1', rating: -1, correction: 'should say 42' });
  });
});
