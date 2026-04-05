import React, { useState, useEffect, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Send, Plus, Trash2, Settings as SettingsIcon, MessageSquare, User, Bot, Loader2, XCircle, ChevronDown, ChevronRight, Brain, Hash, Paperclip, File as FileIcon, X } from 'lucide-react';
// import { getEncoding } from 'js-tiktoken';
import { listLLMsApi, type LLMInfo } from '../api/llm';
import {
  listConversationsApi,
  getConversationApi,
  createConversationApi,
  deleteConversationApi,
  streamCompletionApi,
  type Conversation,
  type Message
} from '../api/chat';
import { Link } from 'react-router-dom';

const ThoughtBlock: React.FC<{ thought: string; defaultOpen?: boolean }> = ({ thought, defaultOpen = false }) => {
  const [isOpen, setIsOpen] = useState(defaultOpen);
  if (!thought) return null;
  return (
    <div className="thought-block">
      <button className="thought-header" onClick={() => setIsOpen(!isOpen)}>
        <Brain size={16} />
        <span>Thought</span>
        {isOpen ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
      </button>
      {isOpen && <div className="thought-content">{thought}</div>}
    </div>
  );
};

export const Chat: React.FC = () => {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [currentConversation, setCurrentConversation] = useState<Conversation | null>(null);
  const [llms, setLLMs] = useState<LLMInfo[]>([]);
  const [selectedModelId, setSelectedModelId] = useState<string>('');
  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [streamingContent, setStreamingContent] = useState('');
  const [streamingThought, setStreamingThought] = useState('');
  const [abortController, setAbortController] = useState<AbortController | null>(null);
  // const [tokenCount, setTokenCount] = useState(0);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  // const enc = useRef(getEncoding('cl100k_base'));

  // useEffect(() => {
  //   setTokenCount(enc.current.encode(input).length);
  // }, [input]);

  useEffect(() => {
    fetchInitialData();
  }, []);

  const fetchInitialData = async () => {
    try {
      const [convs, models] = await Promise.all([
        listConversationsApi(),
        listLLMsApi()
      ]);
      setConversations(convs || []);
      setLLMs(models || []);
      // Default to Auto-Routing ("")
      setSelectedModelId("");
    } catch (err) {
      console.error('Failed to fetch initial data:', err);
    }
  };

  useEffect(() => {
    scrollToBottom();
  }, [currentConversation?.messages, streamingContent, streamingThought]);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  const handleNewChat = () => {
    setCurrentConversation(null);
    setStreamingContent('');
    setStreamingThought('');
    setInput('');
  };

  const loadConversation = async (id: string) => {
    try {
      const conv = await getConversationApi(id);
      setCurrentConversation(conv);
      setSelectedModelId(conv.model_config_id || '');
    } catch (err) {
      alert('Failed to load conversation');
    }
  };

  const handleDeleteConversation = async (e: React.MouseEvent, id: string) => {
    e.stopPropagation();
    if (!window.confirm('Delete this conversation?')) return;
    try {
      await deleteConversationApi(id);
      setConversations(conversations.filter(c => c.id !== id));
      if (currentConversation?.id === id) {
        handleNewChat();
      }
    } catch (err) {
      alert('Failed to delete conversation');
    }
  };

  const triggerFileInput = () => {
    fileInputRef.current?.click();
  };
  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) {
      const files = Array.from(e.target.files);
      setSelectedFiles((prev) => [...prev, ...files]);
    }
  };
  const removeFile = (index: number) => {
    setSelectedFiles((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSend = async () => {
    if (!input.trim() || loading) return;

    const selectedModel = llms.find(m => (m.config.id || m.config.name) === selectedModelId);
    if (selectedModelId !== "" && selectedModel && selectedModel.status !== 'Online') {
      alert(`The selected model "${selectedModel.config.name}" is currently ${selectedModel.status}. Please select an Online model from the dropdown at the top, or use Auto-Routing.`);
      return;
    }

    let conv = currentConversation;
    if (!conv) {
      try {
        const modelName = selectedModel?.config.model_name;
        conv = await createConversationApi(input.substring(0, 30), selectedModelId || undefined, modelName);
        setConversations([conv, ...conversations]);
        setCurrentConversation(conv);
      } catch (err) {
        alert('Failed to create conversation');
        return;
      }
    }

    if (!conv) return;

    const userMsg: Message = { role: 'user', content: input };
    const updatedMessages = [...conv.messages, userMsg];
    setCurrentConversation({ ...conv, messages: updatedMessages });
    setInput('');
    setLoading(true);
    const currentFiles = [...selectedFiles];
    setSelectedFiles([]);
    setStreamingContent('');
    setStreamingThought('');

    const controller = new AbortController();
    setAbortController(controller);

    try {
      await streamCompletionApi(
        conv.id,
        input,
        (thought) => {
          setStreamingThought((prev) => prev + thought);
        },
        (chunk) => {
          setStreamingContent((prev) => prev + chunk);
        },
        async () => {
          if (conv?.id) {
            await loadConversation(conv.id);
          }
          setLoading(false);
          setStreamingContent('');
          setStreamingThought('');
          setAbortController(null);
          fetchInitialData();
        },
        (err) => {
          setLoading(false);
          setStreamingContent('');
          setStreamingThought('');
          setAbortController(null);
          console.error(err);
        },
        currentFiles,
        controller.signal
      );
    } catch (error) {
      console.error('Streaming error:', error);
    }
  };

  const handleStop = () => {
    if (abortController) {
      abortController.abort();
      setAbortController(null);
      setLoading(false);
      setStreamingContent('');
      setStreamingThought('');
    }
  };

  return (
    <div className="chat-layout">
      <aside className="chat-sidebar">
        <button className="new-chat-btn" onClick={handleNewChat}>
          <Plus size={18} /> New Chat
        </button>
        <div className="conversations-list">
          {conversations.map(conv => (
            <div
              key={conv.id}
              className={`conversation-item ${currentConversation?.id === conv.id ? 'active' : ''}`}
              onClick={() => loadConversation(conv.id)}
            >
              <MessageSquare size={16} />
              <span className="conv-title">{conv.title}</span>
              <button className="delete-conv-btn" onClick={(e) => handleDeleteConversation(e, conv.id)}>
                <Trash2 size={14} />
              </button>
            </div>
          ))}
        </div>
        <div className="sidebar-footer">
          <Link to="/settings" className="sidebar-settings-link">
            <SettingsIcon size={16} /> Manage Models
          </Link>
        </div>
      </aside>

      <main className="chat-main">
        {(!llms || llms.length === 0) ? (
          <div className="empty-state-full">
            <Brain size={48} className="empty-icon" />
            <h3>No Models Configured</h3>
            <p>You need at least one model configuration to start chatting.</p>
            <Link to="/settings" className="cta-button">
              Configure Your First Model
            </Link>
          </div>
        ) : !currentConversation ? (
          <div className="welcome-container">
            <header className="chat-header">
              <div className={selectedModelId === "" ? "model-info-badge" : "model-selector-container"}>
                {selectedModelId === "" && <Brain size={14} />}
                <select 
                  className="model-dropdown-simple"
                  value={selectedModelId}
                  onChange={(e) => setSelectedModelId(e.target.value)}
                >
                  <option value="">Auto-Routing (Intelligent)</option>
                  {llms.map((m) => (
                    <option key={m.config.id || m.config.name} value={m.config.id || m.config.name}>
                      {m.config.name} ({m.status})
                    </option>
                  ))}
                </select>
                {selectedModelId !== "" && (
                  <div className={`status-dot ${llms.find(m => (m.config.id || m.config.name) === selectedModelId)?.status === 'Online' ? 'online' : 'offline'}`}></div>
                )}
              </div>
            </header>
            <div className="welcome-content">
              <h1>ConverseAI</h1>
              <p>How can I help you today?</p>
            </div>
            <div className="chat-input-area">
              <div className="input-container-wrapper">
                <div className="input-container">
                  <div className="input-actions-left">
                    <input
                      type="file"
                      multiple
                      ref={fileInputRef}
                      onChange={handleFileChange}
                      style={{ display: 'none' }}
                    />
                    <button
                      type="button"
                      className="action-btn"
                      onClick={triggerFileInput}
                      title="Upload Files"
                    >
                      <Paperclip size={18} />
                    </button>
                  </div>
                  <textarea
                    rows={Math.min(5, input.split('\n').length)}
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault();
                        handleSend();
                      }
                    }}
                    placeholder="Message ConverseAI..."
                    className="chat-textarea"
                  />
                  <button
                    type="submit"
                    className={`send-btn ${(input.trim() || selectedFiles.length > 0) && !loading ? 'active' : ''}`}
                    onClick={loading ? handleStop : handleSend}
                    title={loading ? 'Stop Generating' : 'Send Message'}
                  >
                    {loading ? <XCircle size={18} /> : <Send size={18} />}
                  </button>
                </div>
                {selectedFiles.length > 0 && (
                  <div className="file-preview-list">
                    {selectedFiles.map((file, i) => (
                      <div key={i} className="file-chip">
                        <FileIcon size={12} />
                        <span className="file-name">{file.name}</span>
                        <button className="remove-file-btn" onClick={() => removeFile(i)}>
                          <X size={12} />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </div>
        ) : (
          <>
            <header className="chat-header">
              <div className={selectedModelId === "" ? "model-info-badge" : "model-selector-container"}>
                {selectedModelId === "" && <Brain size={14} />}
                <select 
                  className="model-dropdown-simple"
                  value={selectedModelId}
                  onChange={(e) => setSelectedModelId(e.target.value)}
                >
                  <option value="">Auto-Routing (Intelligent)</option>
                  {llms.map((m) => (
                    <option key={m.config.id || m.config.name} value={m.config.id || m.config.name}>
                      {m.config.name} ({m.status})
                    </option>
                  ))}
                </select>
                {selectedModelId !== "" && (
                  <div className={`status-dot ${llms.find(m => (m.config.id || m.config.name) === selectedModelId)?.status === 'Online' ? 'online' : 'offline'}`}></div>
                )}
              </div>
            </header>

            <div className="messages-container">
              {currentConversation.summary && (
                <div className="summary-banner">
                  <Brain size={16} />
                  <span>Long-term memory active: {currentConversation.summary_token_count} tokens condensed into a summary.</span>
                </div>
              )}
              {currentConversation.messages.map((msg, i) => (
                <div key={i} className={`message-wrapper ${msg.role}`}>
                  <div className="message-content">
                    <div className="message-icon">
                      {msg.role === 'user' ? <User size={20} /> : <Bot size={20} />}
                    </div>
                    <div className="message-text">
                      <ThoughtBlock thought={msg.reasoning || ''} />
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {msg.content}
                      </ReactMarkdown>
                      <div className="message-meta-row">
                        {msg.is_summarized && (
                          <span className="summarized-tag">Summarized</span>
                        )}
                        {msg.token_count !== undefined && msg.token_count > 0 && (
                          <div className="message-token-count">
                            {msg.token_count} tokens
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              ))}

              {(streamingContent || streamingThought) && (
                <div className="message-wrapper assistant">
                  <div className="message-content">
                    <div className="message-icon"><Bot size={20} /></div>
                    <div className="message-text">
                      <ThoughtBlock thought={streamingThought} defaultOpen={true} />
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {streamingContent}
                      </ReactMarkdown>
                    </div>
                  </div>
                </div>
              )}

              {loading && !streamingContent && !streamingThought && (
                <div className="loading-message">
                  <Loader2 className="animate-spin" size={20} />
                  <span>{selectedModelId ? 'Preparing response...' : 'Generating...'}</span>
                </div>
              )}
              {loading && streamingThought && !streamingContent && (
                <div className="loading-message">
                  <Loader2 className="animate-spin" size={20} />
                  <span>Thinking...</span>
                </div>
              )}
              <div ref={messagesEndRef} />
            </div>

            <footer className="chat-input-area">
              <div className="input-container-wrapper">
                {(() => {
                  const selectedModel = llms.find(m => (m.config.id || m.config.name) === selectedModelId);
                  const used = currentConversation?.total_tokens || 0;
                  const max = selectedModel?.config.context_window || 2048;
                  const remaining = max - used;

                  return currentConversation && (
                    <div className="chat-context-indicator" title={`Max: ${max} tokens | Remaining: ${remaining} tokens`}>
                      <Hash size={12} />
                      <span>{used} / {max} tokens used ({Math.max(0, remaining)} remaining)</span>
                    </div>
                  );
                })()}
                <div className="input-container">
                  <div className="input-actions-left">
                    <input
                      type="file"
                      multiple
                      ref={fileInputRef}
                      onChange={handleFileChange}
                      style={{ display: 'none' }}
                    />
                    <button
                      type="button"
                      className="action-btn"
                      onClick={triggerFileInput}
                      title="Upload Files"
                    >
                      <Paperclip size={18} />
                    </button>
                  </div>
                  <textarea
                    rows={1}
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault();
                        handleSend();
                      }
                    }}
                    placeholder="Message ConverseAI..."
                    className="chat-textarea"
                  />
                  <button
                    className={`send-btn ${(input.trim() || selectedFiles.length > 0) && !loading ? 'active' : ''}`}
                    onClick={loading ? handleStop : handleSend}
                    disabled={(!input.trim() && selectedFiles.length === 0) && !loading}
                  >
                    {loading ? <XCircle size={18} /> : <Send size={18} />}
                  </button>
                </div>
                {selectedFiles.length > 0 && (
                  <div className="file-preview-list">
                    {selectedFiles.map((file, i) => (
                      <div key={i} className="file-chip">
                        <FileIcon size={12} />
                        <span className="file-name">{file.name}</span>
                        <button className="remove-file-btn" onClick={() => removeFile(i)}>
                          <X size={12} />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
              </div>
              <p className="input-footer">ConverseAI can make mistakes. Check important info.</p>
            </footer>
          </>
        )}
      </main>

      <style>{`
        .chat-layout { display: flex; height: 100%; background: white; width: 100%; }
        .chat-sidebar { width: 220px; background: #f9fafb; border-right: 1px solid #e2e8f0; display: flex; flex-direction: column; padding: 0.5rem; }
        .new-chat-btn { display: flex; align-items: center; gap: 0.5rem; padding: 0.625rem; border: 1px solid #e2e8f0; border-radius: 0.5rem; background: white; cursor: pointer; transition: all 0.2s; font-weight: 600; margin-bottom: 0.75rem; color: #374151; font-size: 0.8125rem; }
        .new-chat-btn:hover { background: #f3f4f6; border-color: #cbd5e1; }
        .conversations-list { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 0.125rem; }
        .conversation-item { display: flex; align-items: center; gap: 0.625rem; padding: 0.5rem 0.625rem; border-radius: 0.5rem; cursor: pointer; transition: all 0.2s; color: #4b5563; position: relative; font-size: 0.8125rem; }
        .conversation-item:hover { background: #f3f4f6; }
        .conversation-item.active { background: #f1f5f9; color: #111827; font-weight: 500; }
        .conv-title { font-size: 0.8125rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex: 1; }
        .delete-conv-btn { opacity: 0; padding: 0.125rem; background: none; border: none; color: #9ca3af; cursor: pointer; transition: all 0.2s; }
        .conversation-item:hover .delete-conv-btn { opacity: 1; }
        
        .chat-main { flex: 1; display: flex; flex-direction: column; background: white; position: relative; }
        .model-info-badge { display: flex; align-items: center; gap: 0.4rem; background: #f0f9ff; padding: 0.3rem 0.75rem; border-radius: 2rem; border: 1px solid #bae6fd; font-size: 0.75rem; font-weight: 600; color: #0369a1; }

        .messages-container { flex: 1; overflow-y: auto; padding: 1rem 0; }
        .message-wrapper { width: 100%; padding: 0.75rem 1rem; }
        .message-wrapper.user { background: white; }
        .message-wrapper.assistant { background: #f9fafb; }
        .message-content { max-width: 850px; margin: 0 auto; display: flex; gap: 1rem; }
        .message-icon { width: 28px; height: 28px; border-radius: 0.375rem; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
        .user .message-icon { background: #6366f1; color: white; }
        .assistant .message-icon { background: #10b981; color: white; }
        .message-text { flex: 1; font-size: 0.9375rem; line-height: 1.5; color: #1e293b; overflow-wrap: break-word; }
        .message-text p { margin-bottom: 0.625rem; }
        .message-text p:last-child { margin-bottom: 0; }
        .message-text pre { margin: 0.75rem 0; }

        .chat-input-area { padding: 0.5rem 1.5rem 0.75rem; background: white; border-top: 1px solid transparent; }
        .input-container-wrapper { max-width: 850px; margin: 0 auto; display: flex; flex-direction: column; gap: 0.375rem; }
        .chat-context-indicator { display: flex; align-items: center; gap: 0.375rem; font-size: 0.6875rem; color: #64748b; font-weight: 500; padding: 0 0.5rem; }
        .input-container { background: white; border: 1px solid #d1d5db; border-radius: 0.75rem; padding: 0.5rem; display: flex; align-items: flex-end; gap: 0.5rem; box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05); }
        .chat-textarea { flex: 1; min-height: 24px; max-height: 200px; padding: 0.75rem 0.5rem; padding-left: 1rem; border: none; outline: none; background: transparent; font-family: inherit; font-size: 0.9375rem; resize: none; color: #1e293b; line-height: 1.5; }
        .input-actions-left { display: flex; align-items: center; padding: 0 0.5rem; }
        .action-btn { background: none; border: none; color: #64748b; cursor: pointer; padding: 0.5rem; border-radius: 0.5rem; display: flex; align-items: center; justify-content: center; transition: all 0.2s; width: 34px; height: 34px; flex-shrink: 0; }
        .action-btn:hover { background: #f1f5f9; color: #0f172a; }
        .send-btn { background: none; border: none; color: #cbd5e1; cursor: pointer; padding: 0.5rem; display: flex; align-items: center; justify-content: center; transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1); border-radius: 0.5rem; margin-right: 0.25rem; flex-shrink: 0; }
        .send-btn.active { color: #2563eb; background: #eff6ff; }
        .send-btn.active:hover { transform: scale(1.1); transform-origin: center; }

        .model-selector-container { display: flex; align-items: center; gap: 0.5rem; background: #f8fafc; padding: 0.25rem 0.75rem; border-radius: 2rem; border: 1px solid #e2e8f0; }
        .model-dropdown-simple { background: transparent; border: none; font-size: 0.75rem; font-weight: 600; color: #475569; outline: none; cursor: pointer; padding-right: 0.5rem; }
        .status-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
        .status-dot.online { background: #10b981; box-shadow: 0 0 0 2px #d1fae5; }
        .status-dot.offline { background: #ef4444; box-shadow: 0 0 0 2px #fee2e2; }

        .file-preview-list { display: flex; flex-wrap: wrap; gap: 0.5rem; margin-top: 0.5rem; padding: 0 1rem; padding-bottom: 0.5rem; }
        .file-chip { background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 0.5rem; padding: 0.25rem 0.5rem; display: flex; align-items: center; gap: 0.4rem; font-size: 0.75rem; color: #475569; position: relative; transition: all 0.2s; }
        .file-chip:hover { background: #f1f5f9; border-color: #cbd5e1; }
        .file-name { max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .remove-file-btn { background: none; border: none; color: #94a3b8; cursor: pointer; display: flex; align-items: center; justify-content: center; padding: 0.1rem; border-radius: 50%; }
        .remove-file-btn:hover { color: #f43f5e; background: #fff1f2; }
        .message-token-count { font-size: 0.65rem; color: #94a3b8; font-weight: 500; }
        .message-meta-row { display: flex; align-items: center; justify-content: flex-end; gap: 0.5rem; margin-top: 0.25rem; }
        .summarized-tag { font-size: 0.6rem; color: #6366f1; background: #eef2ff; padding: 0.1rem 0.3rem; border-radius: 0.25rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.025em; }
        .summary-banner { max-width: 850px; margin: 0 auto 1rem; padding: 0.75rem 1rem; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 0.75rem; display: flex; align-items: center; gap: 0.75rem; font-size: 0.8125rem; color: #475569; }
        .total-tokens-badge { display: none; }
        .input-footer { text-align: center; font-size: 0.6875rem; color: #94a3b8; margin-top: 0.5rem; }

        .welcome-container { flex: 1; display: flex; flex-direction: column; }
        .welcome-content { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; }
        .welcome-content h1 { font-size: 2rem; font-weight: 700; color: #1e293b; margin-bottom: 0.25rem; }
        .welcome-content p { font-size: 1rem; color: #64748b; }

        .empty-state-full { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; gap: 1rem; padding: 1.5rem; }
        .empty-icon { color: #cbd5e1; }
        .empty-state-full h3 { font-size: 1.25rem; color: #1e293b; margin: 0; }
        .empty-state-full p { color: #64748b; font-size: 0.875rem; max-width: 300px; line-height: 1.5; margin: 0; }
        .cta-button { background: #6366f1; color: white; padding: 0.75rem 1.5rem; border-radius: 0.75rem; text-decoration: none; font-weight: 600; transition: all 0.2s; font-size: 0.875rem; }
        .cta-button:hover { background: #4f46e5; transform: translateY(-1px); box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1); }
        
        .loading-message { max-width: 850px; margin: 0 auto; padding: 0 1rem; color: #64748b; font-size: 0.8125rem; display: flex; align-items: center; gap: 0.375rem; margin-top: 0.5rem;}
        .sidebar-footer { padding: 0.75rem; border-top: 1px solid #e2e8f0; }
        .sidebar-settings-link { display: flex; align-items: center; gap: 0.625rem; color: #4b5563; text-decoration: none; font-size: 0.8125rem; font-weight: 500; padding: 0.5rem; border-radius: 0.5rem; transition: all 0.2s; }
        .sidebar-settings-link:hover { background: #f3f4f6; color: #111827; }
      `}</style>
    </div>
  );
};
