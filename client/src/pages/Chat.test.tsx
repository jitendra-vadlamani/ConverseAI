import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { Conversation, RunHandlers } from '../api/chat';

vi.mock('../api/chat', async importOriginal => {
  const actual = await importOriginal<typeof import('../api/chat')>();
  return {
    ...actual,
    listConversationsApi: vi.fn(),
    listModelsApi: vi.fn(),
    getConversationApi: vi.fn(),
    getEventsApi: vi.fn(),
    listConversationFilesApi: vi.fn(),
    createConversationApi: vi.fn(),
    deleteConversationApi: vi.fn(),
    updateConversationTitleApi: vi.fn(),
    deleteConversationFileApi: vi.fn(),
    streamCompletionApi: vi.fn(),
    attachRunApi: vi.fn(),
    cancelRunApi: vi.fn(),
    submitFeedbackApi: vi.fn(),
  };
});

import * as api from '../api/chat';
import { Chat } from './Chat';

const m = vi.mocked(api);

// EventSource (live System Logs) doesn't exist in jsdom.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  onmessage: ((e: MessageEvent) => void) | null = null;
  closed = false;
  url: string;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  close() {
    this.closed = true;
  }
}

const models = [
  { name: 'Gemma 4', model_name: 'gemma4:latest', context_window: 8192 },
  { name: 'Qwen3 VL 8B', model_name: 'qwen3-vl:8b', context_window: 8192 },
];

function conversation(over: Partial<Conversation> = {}): Conversation {
  return {
    id: 'c1', user_id: 'u1', title: 'First chat', messages: [],
    created_at: '2026-10-03T00:00:00Z', updated_at: '2026-10-03T00:00:00Z', ...over,
  };
}

// A controllable stream: the test drives the handlers and decides when it ends.
function deferredStream() {
  let handlers!: RunHandlers;
  let signal!: AbortSignal;
  let finish!: () => void;
  const impl = (_c: string, _m: string, _t: string, h: RunHandlers, _f?: File[], s?: AbortSignal) => {
    handlers = h;
    signal = s!;
    return new Promise<void>(resolve => {
      finish = resolve;
      s?.addEventListener('abort', () => resolve());
    });
  };
  return { impl, get handlers() { return handlers; }, get signal() { return signal; }, end: () => finish() };
}

function renderChat() {
  return render(<MemoryRouter><Chat /></MemoryRouter>);
}

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal('EventSource', FakeEventSource);
  vi.stubGlobal('alert', vi.fn());
  m.listModelsApi.mockResolvedValue(models);
  m.listConversationsApi.mockResolvedValue([{ id: 'c1', title: 'First chat', created_at: '', updated_at: '' }]);
  m.getEventsApi.mockResolvedValue([]);
  m.listConversationFilesApi.mockResolvedValue([]);
  m.cancelRunApi.mockResolvedValue();
  m.submitFeedbackApi.mockResolvedValue();
});

describe('Chat page', () => {
  it('lists conversations and defaults to the first model', async () => {
    renderChat();
    expect(await screen.findByText('First chat')).toBeInTheDocument();
    expect(screen.getByRole('combobox')).toHaveValue('gemma4:latest');
  });

  it('sends a message, streams the answer and shows the saved reply', async () => {
    const user = userEvent.setup();
    const stream = deferredStream();
    m.createConversationApi.mockResolvedValue(conversation({ id: 'new', title: 'What is Go?' }));
    m.streamCompletionApi.mockImplementation(stream.impl);
    m.getConversationApi.mockResolvedValue(conversation({
      id: 'new', title: 'What is Go?',
      messages: [
        { id: 'u1', role: 'user', content: 'What is Go?', model_name: 'gemma4:latest' },
        { id: 'a1', role: 'assistant', content: 'Go is a **language**.', model_name: 'gemma4:latest' },
      ],
    }));
    renderChat();
    await screen.findByText('First chat');

    await user.type(screen.getByRole('textbox', { name: 'Message' }), 'What is Go?{Enter}');

    await waitFor(() => expect(m.streamCompletionApi).toHaveBeenCalled());
    expect(m.createConversationApi).toHaveBeenCalledWith('What is Go?');
    const [convId, model, text, , files] = m.streamCompletionApi.mock.calls[0];
    expect([convId, model, text, files]).toEqual(['new', 'gemma4:latest', 'What is Go?', []]);
    // The live System Logs stream is opened for this conversation.
    expect(FakeEventSource.instances[0].url).toContain('id=new');

    stream.handlers.onRun?.('run-1');
    stream.handlers.onAnswer('Go is a **lang');
    expect(await screen.findByText(/Go is a/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Stop generating' })).toBeInTheDocument();

    stream.handlers.onDone('succeeded');
    stream.end();

    expect(await screen.findByText('language')).toBeInTheDocument(); // rendered as <strong>
    expect(screen.getByRole('button', { name: 'Send message' })).toBeInTheDocument();
    expect(FakeEventSource.instances[0].closed).toBe(true);
    expect(window.alert).not.toHaveBeenCalled();
  });

  it('stop cancels the run on the server, not just the stream', async () => {
    const user = userEvent.setup();
    const stream = deferredStream();
    m.getConversationApi.mockResolvedValue(conversation());
    m.streamCompletionApi.mockImplementation(stream.impl);
    renderChat();
    await user.click(await screen.findByText('First chat'));
    await user.type(screen.getByRole('textbox', { name: 'Message' }), 'long question{Enter}');
    await waitFor(() => expect(m.streamCompletionApi).toHaveBeenCalled());
    stream.handlers.onRun?.('run-42');

    await user.click(screen.getByRole('button', { name: 'Stop generating' }));

    expect(m.cancelRunApi).toHaveBeenCalledWith('run-42');
    expect(stream.signal.aborted).toBe(true);
    expect(await screen.findByRole('button', { name: 'Send message' })).toBeInTheDocument();
  });

  it('re-attaches to an answer that is still being written', async () => {
    m.getConversationApi.mockResolvedValue(conversation({ active_run_id: 'run-9' }));
    m.attachRunApi.mockImplementation((_id, h) => {
      h.onAnswer('resumed text');
      return new Promise(() => {}); // still running
    });
    const user = userEvent.setup();
    renderChat();
    await user.click(await screen.findByText('First chat'));

    await waitFor(() => expect(m.attachRunApi).toHaveBeenCalled());
    expect(m.attachRunApi.mock.calls[0][0]).toBe('run-9');
    expect(await screen.findByText('resumed text')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Stop generating' })).toBeInTheDocument();
  });

  it('shows the server error when an answer fails', async () => {
    const user = userEvent.setup();
    m.getConversationApi.mockResolvedValue(conversation());
    m.streamCompletionApi.mockImplementation(async (_c, _m, _t, h) => {
      h.onError('the model server failed to answer; please try again');
      h.onDone('failed');
    });
    renderChat();
    await user.click(await screen.findByText('First chat'));
    await user.type(screen.getByRole('textbox', { name: 'Message' }), 'hi{Enter}');
    await waitFor(() => expect(window.alert).toHaveBeenCalledWith('Error: the model server failed to answer; please try again'));
  });

  it('records thumbs up and thumbs down with a correction', async () => {
    const user = userEvent.setup();
    m.getConversationApi.mockResolvedValue(conversation({
      messages: [
        { id: 'u1', role: 'user', content: 'Capital of France?', model_name: 'gemma4:latest' },
        { id: 'a1', role: 'assistant', content: 'Lyon.', model_name: 'gemma4:latest' },
      ],
    }));
    vi.stubGlobal('prompt', vi.fn(() => 'It is Paris.'));
    renderChat();
    await user.click(await screen.findByText('First chat'));
    await screen.findByText('Lyon.');

    // Only assistant messages get feedback buttons.
    expect(screen.getAllByRole('button', { name: 'Good answer' })).toHaveLength(1);

    await user.click(screen.getByRole('button', { name: 'Bad answer' }));
    expect(m.submitFeedbackApi).toHaveBeenCalledWith('c1', 'a1', -1, 'It is Paris.');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Bad answer' })).toHaveAttribute('aria-pressed', 'true'));

    await user.click(screen.getByRole('button', { name: 'Good answer' }));
    expect(m.submitFeedbackApi).toHaveBeenLastCalledWith('c1', 'a1', 1, '');
  });

  it('lists conversation files and deletes one after confirmation', async () => {
    const user = userEvent.setup();
    const fileID = 'user-u1/1791022981511962984-report.pdf';
    m.getConversationApi.mockResolvedValue(conversation({
      messages: [{ id: 'u1', role: 'user', content: 'see attached', model_name: 'gemma4:latest', attachments: [fileID] }],
    }));
    m.listConversationFilesApi.mockResolvedValue([fileID]);
    m.deleteConversationFileApi.mockResolvedValue();
    vi.stubGlobal('confirm', vi.fn(() => true));
    renderChat();
    await user.click(await screen.findByText('First chat'));

    await user.click(await screen.findByRole('button', { name: 'Files' }));
    const grid = await screen.findByText('1 files');
    expect(grid).toBeInTheDocument();
    const card = screen.getAllByText('report.pdf').at(-1)!.closest('.file-card') as HTMLElement;
    await user.click(within(card).getByText('Delete'));

    expect(m.deleteConversationFileApi).toHaveBeenCalledWith('c1', fileID);
    expect(await screen.findByText('0 files')).toBeInTheDocument();
  });

  it('does not delete a conversation unless confirmed', async () => {
    const user = userEvent.setup();
    vi.stubGlobal('confirm', vi.fn(() => false));
    renderChat();
    await user.click(await screen.findByRole('button', { name: 'Delete First chat' }));
    expect(m.deleteConversationApi).not.toHaveBeenCalled();
    expect(screen.getByText('First chat')).toBeInTheDocument();
  });

  it('renames a conversation with Enter', async () => {
    const user = userEvent.setup();
    m.updateConversationTitleApi.mockResolvedValue();
    renderChat();
    await user.click(await screen.findByRole('button', { name: 'Rename First chat' }));
    const input = screen.getByDisplayValue('First chat');
    await user.clear(input);
    await user.type(input, 'Renamed{Enter}');
    expect(m.updateConversationTitleApi).toHaveBeenCalledWith('c1', 'Renamed');
    expect(await screen.findByText('Renamed')).toBeInTheDocument();
  });
});
