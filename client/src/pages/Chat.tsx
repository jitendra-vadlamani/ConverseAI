import React, { useState, useEffect, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Send, Plus, Trash2, Settings as SettingsIcon, MessageSquare, User, Bot, Loader2, XCircle, ChevronDown, ChevronRight, Brain } from 'lucide-react';
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
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [streamingContent, setStreamingContent] = useState('');
  const [streamingThought, setStreamingThought] = useState('');
  const [abortController, setAbortController] = useState<AbortController | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetchInitialData();
  }, []);

  const fetchInitialData = async () => {
    try {
      const [convs, models] = await Promise.all([
        listConversationsApi(),
        listLLMsApi()
      ]);
      setConversations(convs);
      setLLMs(models);
      if (models.length > 0) {
        setSelectedModelId(models[0].config.id || '');
      }
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

  const handleSend = async () => {
    if (!input.trim() || loading) return;

    const selectedModel = llms.find(m => m.config.id === selectedModelId || (!m.config.id && selectedModelId === ''));
    if (selectedModel && selectedModel.status !== 'Online') {
      alert(`The selected model is currently ${selectedModel.status}. Please select an Online model.`);
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
        () => {
          setLoading(false);
          setStreamingContent('');
          setStreamingThought('');
          setAbortController(null);
          if (conv?.id) {
            loadConversation(conv.id);
          }
          fetchInitialData();
        },
        (err) => {
          if (err !== 'signal is aborted' && err !== 'The user aborted a request.') {
            alert(err);
          }
          setLoading(false);
          setStreamingContent('');
          setStreamingThought('');
          setAbortController(null);
        },
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
        {llms.length === 0 ? (
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
               <div className="model-selector">
                  <span className="model-label">Using:</span>
                  <select 
                    value={selectedModelId} 
                    onChange={(e) => setSelectedModelId(e.target.value)}
                  >
                    {llms.map(llm => (
                      <option key={llm.config.id || llm.config.model_name} value={llm.config.id || ''}>
                        {llm.config.name} ({llm.status})
                      </option>
                    ))}
                  </select>
                  <ChevronDown size={14} className="selector-icon" />
               </div>
            </header>
            <div className="welcome-content">
              <h1>ConverseAI</h1>
              <p>How can I help you today?</p>
            </div>
            <div className="chat-input-area">
              <div className="input-container">
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
                  type="submit" 
                  className={`send-btn ${input.trim() || loading ? 'active' : ''}`}
                  onClick={loading ? handleStop : handleSend}
                  title={loading ? 'Stop Generating' : 'Send Message'}
                >
                  {loading ? <XCircle size={18} /> : <Send size={18} />}
                </button>
              </div>
            </div>
          </div>
        ) : (
          <>
            <header className="chat-header">
               <div className="model-selector">
                  <span className="model-label">Using:</span>
                  <select 
                    value={selectedModelId} 
                    onChange={(e) => setSelectedModelId(e.target.value)}
                    disabled={loading || !!currentConversation}
                  >
                    {llms.map(llm => (
                      <option key={llm.config.id} value={llm.config.id}>
                        {llm.config.name} ({llm.config.model_name})
                      </option>
                    ))}
                  </select>
                  <ChevronDown size={14} className="selector-icon" />
               </div>
            </header>

            <div className="messages-container">
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
              <div className="input-container">
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
                  className={`send-btn ${input.trim() && !loading ? 'active' : ''}`}
                  onClick={handleSend}
                  disabled={!input.trim() || loading}
                >
                  {loading ? <Loader2 className="animate-spin" size={18} /> : <Send size={18} />}
                </button>
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
        .chat-header { padding: 0.5rem 1rem; border-bottom: 1px solid #e2e8f0; display: flex; align-items: center; }
        .model-selector { display: flex; align-items: center; gap: 0.35rem; background: #f8fafc; padding: 0.25rem 0.5rem; border-radius: 0.5rem; border: 1px solid #e2e8f0; position: relative; }
        .model-label { font-size: 0.6875rem; color: #64748b; font-weight: 500; }
        .model-selector select { background: none; border: none; font-size: 0.75rem; font-weight: 600; color: #1e293b; appearance: none; padding-right: 1.125rem; cursor: pointer; outline: none; }
        .selector-icon { position: absolute; right: 0.35rem; pointer-events: none; color: #64748b; }

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

        .chat-input-area { padding: 0.75rem 1.5rem; background: white; border-top: 1px solid transparent; }
        .input-container { max-width: 850px; margin: 0 auto; background: white; border: 1px solid #d1d5db; border-radius: 0.75rem; padding: 0.5rem; display: flex; align-items: flex-end; gap: 0.5rem; box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05); }
        .chat-textarea { flex: 1; border: none; resize: none; padding: 0.25rem; font-size: 1rem; line-height: 1.5; outline: none; max-height: 200px; }
        .send-btn { background: #e2e8f0; color: #94a3b8; border: none; padding: 0.5rem; border-radius: 0.375rem; cursor: pointer; transition: all 0.2s; display: flex; align-items: center; justify-content: center; }
        .send-btn.active { background: #6366f1; color: white; }
        .send-btn.active:hover { background: #4f46e5; transform: scale(1.05); }
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
