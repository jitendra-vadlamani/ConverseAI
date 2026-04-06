import React, { useState, useEffect, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Send, Plus, Trash2, Edit3, Settings as SettingsIcon, MessageSquare, User, Bot, Loader2, XCircle, ChevronDown, ChevronRight, Brain, Paperclip, File as FileIcon, X, Search, FileText } from 'lucide-react';
// import { getEncoding } from 'js-tiktoken';
import {
  listConversationsApi,
  getConversationApi,
  createConversationApi,
  deleteConversationApi,
  streamCompletionApi,
  listModelsApi,
  updateConversationTitleApi,
  listConversationFilesApi,
  deleteConversationFileApi,
  type Message,
  type ConversationEvent,
  type Evidence
} from '../api/chat';
import { Link } from 'react-router-dom';
import { FileCard } from '../components/FileCard';

type ThoughtBlockProps = {
  thought: string;
  defaultOpen?: boolean;
};

const ThoughtBlock: React.FC<ThoughtBlockProps> = ({ thought, defaultOpen = false }) => {
  const [isOpen, setIsOpen] = useState(isStreamingThought(thought, defaultOpen));
  
  function isStreamingThought(t: string, def: boolean): boolean {
    return t.length > 0 ? true : def;
  }

  // Effect to open when thought starts arriving
  useEffect(() => {
    if (thought.length > 0 && !isOpen) {
      setIsOpen(true);
    }
  }, [thought]);

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

const SourceBadge: React.FC<{ name: string; url?: string }> = ({ name, url }: { name: string; url?: string }) => {
  const handleClick = (e: React.MouseEvent) => {
    if (url) {
      e.preventDefault();
      window.open(url, '_blank', 'noopener,noreferrer');
    }
  };

  return (
    <span 
      className={`source-badge ${url ? 'clickable' : ''}`} 
      onClick={handleClick}
      title={url ? `Open source: ${url}` : `Source: ${name}`}
    >
      <FileText size={10} />
      <span>{name}</span>
    </span>
  );
};

const MarkdownContent: React.FC<{ content: string }> = ({ content }: { content: string }) => {
  // Regex to match [Source: Name] or [1] etc.
  // We'll roughly replace them with placeholders that ReactMarkdown components can then pick up
  // Actually, a simpler way in v1 is to just parse the string before rendering
  // but let's try to use the 'components' prop for a more robust approach.
  
  // Custom component for text to handle citation markers [Source: Name]
  const renderers = {
    text: ({ value }: { value: string }) => {
      const parts = value.split(/(\[Source:\s*[^\]]+\]|\[\d+\])/g);
      return (
        <>
          {parts.map((part, i) => {
            const match = part.match(/\[Source:\s*([^\]]+)\]/);
            const numMatch = part.match(/\[(\d+)\]/);
            if (match) {
              return <SourceBadge key={i} name={match[1]} url={part.includes('http') ? match[1] : undefined} />;
            }
            if (numMatch) {
              return <SourceBadge key={i} name={numMatch[1]} />;
            }
            return part;
          })}
        </>
      );
    }
  };

  return (
    <ReactMarkdown 
      remarkPlugins={[remarkGfm]}
      components={renderers as any}
    >
      {content}
    </ReactMarkdown>
  );
};

interface EventItemProps {
  event: ConversationEvent;
}

const EventItem: React.FC<EventItemProps> = ({ event }) => {
  const [isRawOpen, setIsRawOpen] = useState(false);
  const message: string = event.payload?.message || event.type.replace(/_/g, ' ');
  
  const getIcon = (): React.ReactNode => {
    switch (event.type) {
      case 'rag_search_started':
      case 'rag_search_finished':
        return <Search size={14} />;
      case 'search_started':
      case 'search_finished':
        return <Search size={14} className="text-blue-500" />;
      case 'extraction_started':
      case 'extraction_finished':
        return <FileText size={14} className="text-orange-500" />;
      case 'sufficiency_checked':
        return <Brain size={14} className="text-green-500" />;
      case 'grounded_generation_started':
        return <Loader2 size={14} className="animate-spin text-purple-500" />;
      case 'planner_output':
      case 'orchestration_started':
        return <Brain size={14} />;
      case 'task_started':
      case 'task_finished':
        return <Loader2 size={14} className={event.type === 'task_started' ? 'animate-spin' : ''} />;
      case 'assistant_message_generated':
        return <Bot size={14} />;
      case 'attachment_resolved':
        return <Paperclip size={14} />;
      default:
        return <MessageSquare size={14} />;
    }
  };

  const renderSufficiencyResult = () => {
    if (event.type !== 'sufficiency_checked' || !event.payload) return null;
    const { covered, missing, score } = event.payload as any;
    return (
      <div className="sufficiency-view">
        <div className="sufficiency-score">
          Confidence: <span className="score-val">{(score * 100).toFixed(0)}%</span>
        </div>
        <div className="sufficiency-grid">
          <div className="aspect-col covered">
            <header>Found</header>
            <ul>{Array.isArray(covered) && covered.map((a: string, i: number) => <li key={i}>{a}</li>)}</ul>
          </div>
          <div className="aspect-col missing">
            <header>Missing</header>
            <ul>{Array.isArray(missing) && missing.map((a: string, i: number) => <li key={i}>{a}</li>)}</ul>
          </div>
        </div>
      </div>
    );
  };

  const renderSearchMetadata = () => {
    if ((event.type !== 'search_finished' && event.type !== 'rag_search_finished') || !event.payload) return null;
    const results = event.payload.results || [];
    if (!Array.isArray(results) || results.length === 0) return null;

    return (
      <div className="search-metadata-view">
        {results.map((res: any, i: number) => (
          <div key={i} className={`search-result-item ${res.is_conflicting ? 'conflicting' : ''}`}>
            <header>
              <span className="source-label">{res.source}</span>
              <span className="score-badge main">Score: {(res.final_score * 100).toFixed(0)}%</span>
              {res.is_conflicting && <span className="conflict-tag">CONFLICT</span>}
            </header>
            {res.is_conflicting && <p className="conflict-reason">{res.conflict_reason}</p>}
            <div className="metrics">
              <span>Auth: {(res.authority_score * 100).toFixed(0)}%</span>
              <span>Fresh: {(res.freshness_score * 100).toFixed(0)}%</span>
            </div>
          </div>
        ))}
      </div>
    );
  };

  return (
    <div className="event-item">
      <div className="event-header" onClick={() => setIsRawOpen(!isRawOpen)}>
        {getIcon()}
        <span className="event-type">{message}</span>
        <span className="event-time">{new Date(event.timestamp).toLocaleTimeString()}</span>
      </div>
      {isRawOpen && (
        <div className="event-details">
          {renderSufficiencyResult()}
          {renderSearchMetadata()}
          <pre>{JSON.stringify(event.payload, null, 2)}</pre>
        </div>
      )}
    </div>
  );
};

export const Chat: React.FC = () => {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [currentConversation, setCurrentConversation] = useState<Conversation | null>(null);
  const [models, setModels] = useState<any[]>([]);
  const [selectedModelName, setSelectedModelName] = useState<string>('');
  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [streamingContent, setStreamingContent] = useState('');
  const [streamingThought, setStreamingThought] = useState('');
  const [abortController, setAbortController] = useState<AbortController | null>(null);
  const [activeTab, setActiveTab] = useState<'chat' | 'events' | 'files'>('chat');
  const [events, setEvents] = useState<ConversationEvent[]>([]);
  const [conversationFiles, setConversationFiles] = useState<string[]>([]);
  const eventsEndRef = useRef<HTMLDivElement>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingTitle, setEditingTitle] = useState<string>('');

  // useEffect(() => {
  //   setTokenCount(enc.current.encode(input).length);
  // }, [input]);

  useEffect(() => {
    fetchInitialData();
  }, []);

  const fetchInitialData = async () => {
    try {
      const [convs, systemModels] = await Promise.all([
        listConversationsApi(),
        listModelsApi()
      ]);
      setConversations(convs || []);
      setModels(systemModels || []);
      // Default to first model or Auto-Routing
      if (systemModels && systemModels.length > 0) {
        setSelectedModelName(systemModels[0].model_name);
      }
    } catch (err) {
      console.error('Failed to fetch initial data:', err);
    }
  };

  useEffect(() => {
    scrollToBottom();
  }, [currentConversation?.messages, streamingContent, streamingThought, events, activeTab]);

  const scrollToBottom = (): void => {
    if (activeTab === 'chat') {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    } else {
      eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  };

  const handleNewChat = () => {
    setCurrentConversation(null);
    setStreamingContent('');
    setStreamingThought('');
    setInput('');
    setEvents([]);
  };

  const loadConversation = async (id: string): Promise<void> => {
    try {
      setEvents([]);
      const [conv, historicalEvents, files] = await Promise.all([
        getConversationApi(id),
        fetch(`/api/chat/conversations/events?id=${id}`).then((r: Response) => r.json()),
        listConversationFilesApi(id)
      ]);
      setCurrentConversation(conv);
      setEvents(historicalEvents || []);
      setConversationFiles(files || []);
      // Derive current model from last message if available
      if (conv.messages && conv.messages.length > 0) {
        const lastMsg = conv.messages[conv.messages.length - 1];
        if (lastMsg.model_name) {
          setSelectedModelName(lastMsg.model_name);
        }
      }
    } catch (err) {
      alert('Failed to load conversation');
    }
  };
  
  const startEditing = (e: React.MouseEvent, conv: Conversation) => {
    e.stopPropagation();
    setEditingId(conv.id);
    setEditingTitle(conv.title);
  };

  const handleUpdateTitle = async (id: string) => {
    if (!editingTitle.trim() || editingTitle === conversations.find(c => c.id === id)?.title) {
      setEditingId(null);
      return;
    }
    
    try {
      await updateConversationTitleApi(id, editingTitle.trim());
      setConversations(conversations.map(c => c.id === id ? { ...c, title: editingTitle.trim() } : c));
      if (currentConversation?.id === id) {
        setCurrentConversation({ ...currentConversation, title: editingTitle.trim() });
      }
    } catch (err) {
      alert('Failed to update title');
    } finally {
      setEditingId(null);
    }
  };

  const handleUpdateTitleKeyDown = (e: React.KeyboardEvent, id: string) => {
    if (e.key === 'Enter') {
      handleUpdateTitle(id);
    } else if (e.key === 'Escape') {
      setEditingId(null);
    }
  };

  const handleDeleteConversation = async (e: React.MouseEvent, id: string): Promise<void> => {
    e.stopPropagation();
    if (!window.confirm('Delete this conversation?')) return;
    try {
      await deleteConversationApi(id);
      setConversations(conversations.filter((c: Conversation) => c.id !== id));
      if (currentConversation?.id === id) {
        handleNewChat();
      }
    } catch (err) {
      alert('Failed to delete conversation');
    }
  };

  const triggerFileInput = (): void => {
    fileInputRef.current?.click();
  };
  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>): void => {
    if (e.target.files) {
      const files = Array.from(e.target.files);
      setSelectedFiles((prev: File[]) => [...prev, ...files]);
    }
  };
  const removeFile = (index: number): void => {
    setSelectedFiles((prev: File[]) => prev.filter((_: File, i: number) => i !== index));
  };

  const handleSend = async () => {
    if (!input.trim() || loading) return;

    const selectedModel = models.find(m => m.model_name === selectedModelName);
    if (!selectedModel) {
      alert(`Model ${selectedModelName} not found.`);
      return;
    }

    let conv = currentConversation;
    if (!conv) {
      try {
        conv = await createConversationApi(input.substring(0, 30));
        setConversations([conv, ...conversations]);
        setCurrentConversation(conv);
      } catch (err) {
        alert('Failed to create conversation');
        return;
      }
    }

    if (!conv) return;

    const userMsg: Message = { role: 'user', content: input, model_name: selectedModelName };
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

    // Subscribe to Event Stream
    const eventSource = new EventSource(`/api/chat/conversations/events/stream?id=${conv.id}`);
    eventSource.onmessage = (e: MessageEvent) => {
      try {
        const event: ConversationEvent = JSON.parse(e.data);
        setEvents((prev: ConversationEvent[]) => [...prev, event]);
      } catch (err) {}
    };

    try {
      await streamCompletionApi(
        conv.id,
        selectedModelName,
        input,
        (thought: string) => {
          setStreamingThought((prev: string) => prev + thought);
        },
        (chunk: string) => {
          setStreamingContent((prev: string) => prev + chunk);
        },
        async () => {
          eventSource.close();
          if (conv?.id) {
            await Promise.all([
              loadConversation(conv.id),
              listConversationFilesApi(conv.id).then(setConversationFiles)
            ]);
          }
          setLoading(false);
          setStreamingContent('');
          setStreamingThought('');
          setAbortController(null);
          fetchInitialData();
        },
        (err: string) => {
          eventSource.close();
          setLoading(false);
          setStreamingContent('');
          setStreamingThought('');
          setAbortController(null);
          alert(`Error: ${err}`);
          console.error(err);
        },
        currentFiles,
        controller.signal
      );
    } catch (error: any) {
      eventSource.close();
      alert(`Streaming error: ${error.message || error}`);
      console.error('Streaming error:', error);
    }
  };

  const handleStop = (): void => {
    if (abortController) {
      abortController.abort();
      setAbortController(null);
    }
    setLoading(false);
    setStreamingContent('');
    setStreamingThought('');
  };

  const handleDeleteFile = async (fileID: string) => {
    if (!currentConversation) return;
    if (!window.confirm(`Delete this file? It will be removed from this conversation. If not used elsewhere, it will be permanently deleted.`)) return;

    try {
      await deleteConversationFileApi(currentConversation.id, fileID);
      // Update local state
      setConversationFiles(prev => prev.filter(f => f !== fileID));
      // Also update messages in currentConversation to remove the attachment visually
      if (currentConversation.messages) {
        const updatedMessages = currentConversation.messages.map(msg => ({
          ...msg,
          attachments: msg.attachments?.filter(a => a !== fileID)
        }));
        setCurrentConversation({ ...currentConversation, messages: updatedMessages });
      }
    } catch (err) {
      alert('Failed to delete file');
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
              onClick={() => editingId !== conv.id && loadConversation(conv.id)}
            >
              <MessageSquare size={16} />
              {editingId === conv.id ? (
                <input
                  className="edit-title-input"
                  value={editingTitle}
                  onChange={(e) => setEditingTitle(e.target.value)}
                  onKeyDown={(e) => handleUpdateTitleKeyDown(e, conv.id)}
                  onBlur={() => handleUpdateTitle(conv.id)}
                  autoFocus
                  onClick={(e) => e.stopPropagation()}
                />
              ) : (
                <>
                  <span className="conv-title" title={conv.title}>{conv.title}</span>
                  <div className="conv-actions">
                    <button className="edit-conv-btn" onClick={(e) => startEditing(e, conv)} title="Rename">
                      <Edit3 size={14} />
                    </button>
                    <button className="delete-conv-btn" onClick={(e) => handleDeleteConversation(e, conv.id)} title="Delete">
                      <Trash2 size={14} />
                    </button>
                  </div>
                </>
              )}
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
        {(!models || models.length === 0) ? (
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
              <div className="model-selector-container">
                <Brain size={14} />
                <select 
                   className="model-dropdown-simple"
                   value={selectedModelName}
                   onChange={(e) => setSelectedModelName(e.target.value)}
                 >
                   {models.map((m) => (
                     <option key={m.model_name} value={m.model_name}>
                       {m.name || m.model_name}
                     </option>
                   ))}
                 </select>
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
              <div className="model-selector-container">
                <Brain size={14} />
                <select 
                  className="model-dropdown-simple"
                  value={selectedModelName}
                  onChange={(e) => setSelectedModelName(e.target.value)}
                >
                  {models.map((m) => (
                    <option key={m.model_name} value={m.model_name}>
                      {m.name || m.model_name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="chat-tabs">
                <button 
                  className={`tab-btn ${activeTab === 'chat' ? 'active' : ''}`}
                  onClick={() => setActiveTab('chat')}
                >Chat</button>
                <button 
                  className={`tab-btn ${activeTab === 'events' ? 'active' : ''}`}
                  onClick={() => setActiveTab('events')}
                >System Logs</button>
                <button 
                  className={`tab-btn ${activeTab === 'files' ? 'active' : ''}`}
                  onClick={() => setActiveTab('files')}
                >Files</button>
              </div>
            </header>

            {activeTab === 'chat' ? (
              <div className="messages-container">
                {currentConversation.summary && (
                  <div className="summary-banner">
                    <Brain size={16} />
                    <span>Long-term memory active: Conversation condensed into a summary.</span>
                  </div>
                )}
                {currentConversation.messages.map((msg, i) => (
                  <div key={i} className={`message-wrapper ${msg.role}`}>
                    <div className="message-content">
                      <div className="message-icon">
                        {msg.role === 'user' ? <User size={20} /> : <Bot size={20} />}
                      </div>
                      <div className="message-text">
                        <div className="message-meta-row">
                          {msg.model_name && <span className="message-model-tag">{msg.model_name}</span>}
                          {msg.is_summarized && <span className="summarized-tag">Summarized</span>}
                        </div>
                        <ThoughtBlock thought={msg.reasoning || ''} />
                        <MarkdownContent content={msg.content} />
                        {msg.attachments && msg.attachments.length > 0 && (
                          <div className="message-attachments">
                            {msg.attachments.map((att, idx) => (
                              <FileCard key={idx} fileID={att} />
                            ))}
                          </div>
                        )}
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
                        <MarkdownContent content={streamingContent} />
                      </div>
                    </div>
                  </div>
                )}

                {loading && !streamingContent && !streamingThought && (
                  <div className="loading-message">
                    <Loader2 className="animate-spin" size={20} />
                    <span>Using {selectedModelName}...</span>
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
            ) : activeTab === 'events' ? (
              <div className="events-view-container">
                <div className="events-header">
                  <div className="events-title">
                    <MessageSquare size={18} />
                    <span>Processing Events</span>
                  </div>
                  <span className="events-count">{events.length} events logged</span>
                </div>
                <div className="events-list">
                  {events.length > 0 ? (
                    events.map((ev, i) => (
                      <EventItem key={i} event={ev} />
                    ))
                  ) : (
                    <div className="empty-events-state">
                      <Loader2 size={32} className="animate-spin" />
                      <p>Waiting for processing events...</p>
                    </div>
                  )}
                  <div ref={eventsEndRef} />
                </div>
              </div>
            ) : (
              <div className="files-view-container">
                <div className="files-view-header">
                  <div className="files-view-title">
                    <FileText size={18} />
                    <span>Files in this Conversation</span>
                  </div>
                  <span className="files-count">{conversationFiles.length} files</span>
                </div>
                <div className="files-grid">
                  {conversationFiles.length > 0 ? (
                    conversationFiles.map((fileID: string, idx: number) => (
                      <FileCard 
                        key={idx} 
                        fileID={fileID} 
                        onDelete={() => handleDeleteFile(fileID)} 
                      />
                    ))
                  ) : (
                    <div className="empty-files-state">
                      <Paperclip size={32} />
                      <p>No files uploaded in this conversation yet.</p>
                    </div>
                  )}
                </div>
              </div>
            )}

            <footer className="chat-input-area">
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
        .conv-actions { display: flex; gap: 0.25rem; opacity: 0; transition: opacity 0.2s; }
        .conversation-item:hover .conv-actions { opacity: 1; }
        .edit-conv-btn, .delete-conv-btn { padding: 0.25rem; background: none; border: none; color: #9ca3af; cursor: pointer; transition: all 0.2s; border-radius: 0.25rem; display: flex; align-items: center; justify-content: center; }
        .edit-conv-btn:hover { color: #6366f1; background: #eef2ff; }
        .delete-conv-btn:hover { color: #ef4444; background: #fef2f2; }
        .edit-title-input { flex: 1; min-width: 0; background: white; border: 1px solid #6366f1; border-radius: 0.25rem; padding: 0.125rem 0.375rem; font-size: 0.8125rem; outline: none; box-shadow: 0 0 0 2px rgba(99, 102, 241, 0.1); }
        
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
        .message-model-tag { font-size: 0.65rem; color: #94a3b8; background: #f8fafc; padding: 0.1rem 0.4rem; border-radius: 0.25rem; font-weight: 700; text-transform: uppercase; border: 1px solid #e2e8f0; }
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
        .message-attachments {
          display: flex;
          flex-wrap: wrap;
          gap: 0.5rem;
          margin-top: 0.75rem;
        }
        .summary-banner { max-width: 850px; margin: 0 auto 1rem; padding: 0.75rem 1rem; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 0.75rem; display: flex; align-items: center; gap: 0.75rem; font-size: 0.8125rem; color: #475569; }
        .total-tokens-badge { display: none; }
        .input-footer { text-align: center; font-size: 0.6875rem; color: #94a3b8; margin-top: 0.5rem; }

        .chat-tabs { display: flex; gap: 0.25rem; background: #f1f5f9; padding: 0.2rem; border-radius: 0.5rem; }
        .tab-btn { padding: 0.3rem 0.75rem; border-radius: 0.375rem; border: none; background: transparent; font-size: 0.75rem; font-weight: 600; color: #64748b; cursor: pointer; transition: all 0.2s; }
        .tab-btn.active { background: white; color: #0f172a; box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05); }

        .events-container { flex: 1; overflow-y: auto; display: flex; flex-direction: column; background: #0f172a; }
        .events-header { padding: 0.75rem 1.5rem; border-bottom: 1px solid #1e293b; display: flex; justify-content: space-between; align-items: center; background: #1e293b; }
        .events-title { font-size: 0.8125rem; font-weight: 600; color: #e2e8f0; }
        .events-count { font-size: 0.6875rem; color: #94a3b8; }
        .events-list { flex: 1; padding: 1rem; display: flex; flex-direction: column; gap: 0.75rem; }
        .event-item { background: #1e293b; border-radius: 0.5rem; padding: 0.75rem; border-left: 3px solid #3b82f6; }
        .event-meta { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
        .event-type-wrapper { display: flex; align-items: center; gap: 0.4rem; }
        .event-icon { color: #3b82f6; display: flex; align-items: center; }
        .event-time { font-size: 0.65rem; color: #94a3b8; font-family: monospace; }
        .event-type { font-size: 0.65rem; font-weight: 700; color: #3b82f6; text-transform: uppercase; letter-spacing: 0.05em; }
        .event-content { display: flex; flex-direction: column; gap: 0.4rem; }
        .event-message { font-size: 0.8125rem; color: #e2e8f0; font-weight: 500; }
        .event-raw-toggle { align-self: flex-start; background: none; border: none; font-size: 0.65rem; color: #64748b; cursor: pointer; padding: 0; text-decoration: underline; margin-top: 0.2rem; }
        .event-raw-toggle:hover { color: #94a3b8; }
        .event-payload { font-size: 0.75rem; color: #cbd5e1; background: #0f172a; padding: 0.5rem; border-radius: 0.25rem; overflow-x: auto; margin-top: 0.5rem; }
        .event-payload pre { margin: 0; white-space: pre-wrap; word-break: break-all; }
        
        .event-item.rag_search_started { border-left-color: #8b5cf6; }
        .event-item.rag_search_started .event-icon, .event-item.rag_search_started .event-type { color: #8b5cf6; }
        .event-item.rag_search_finished { border-left-color: #a78bfa; }
        .event-item.rag_search_finished .event-icon, .event-item.rag_search_finished .event-type { color: #a78bfa; }
        .event-item.planner_output { border-left-color: #10b981; }
        .event-item.planner_output .event-icon, .event-item.planner_output .event-type { color: #10b981; }
        .event-item.orchestration_started { border-left-color: #f59e0b; }
        .event-item.orchestration_started .event-icon, .event-item.orchestration_started .event-type { color: #f59e0b; }
        .event-item.task_finished { border-left-color: #3b82f6; }

        .files-view-container { flex: 1; overflow-y: auto; display: flex; flex-direction: column; background: #f8fafc; }
        .files-view-header { padding: 1rem 1.5rem; border-bottom: 1px solid #e2e8f0; display: flex; justify-content: space-between; align-items: center; background: white; }
        .files-view-title { display: flex; align-items: center; gap: 0.5rem; font-size: 0.9375rem; font-weight: 600; color: #1e293b; }
        .files-count { font-size: 0.75rem; color: #64748b; font-weight: 500; }
        .files-grid { padding: 1.5rem; display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 1rem; align-content: flex-start; }
        .empty-files-state { grid-column: 1 / -1; display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 4rem 2rem; color: #94a3b8; gap: 1rem; text-align: center; }
        .empty-files-state p { font-size: 0.875rem; }

        .events-view-container { flex: 1; overflow-y: auto; display: flex; flex-direction: column; background: white; }
        .events-header { padding: 1rem 1.5rem; border-bottom: 1px solid #f1f5f9; display: flex; justify-content: space-between; align-items: center; }
        .events-title { display: flex; align-items: center; gap: 0.5rem; font-size: 0.9375rem; font-weight: 600; color: #1e293b; }
        .events-count { font-size: 0.75rem; color: #64748b; }
        .events-list { flex: 1; padding: 1rem 1.5rem; display: flex; flex-direction: column; gap: 0.75rem; }
        .empty-events-state { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; color: #94a3b8; gap: 1rem; }
        
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

        /* New Reasoning & Search Styles */
        .source-badge {
          display: inline-flex;
          align-items: center;
          gap: 0.25rem;
          background: #f1f5f9;
          color: #475569;
          padding: 0.1rem 0.4rem;
          border-radius: 0.25rem;
          font-size: 0.75rem;
          font-weight: 600;
          margin: 0 0.2rem;
          border: 1px solid #e2e8f0;
          vertical-align: middle;
          cursor: default;
          transition: all 0.2s;
        }
        .source-badge.clickable {
          cursor: pointer;
          color: #2563eb;
          border-color: #bfdbfe;
          background: #eff6ff;
        }
        .source-badge.clickable:hover {
          background: #dbeafe;
          border-color: #3b82f6;
        }

        .sufficiency-view {
          margin-top: 0.5rem;
          background: #111827;
          border-radius: 0.5rem;
          padding: 0.75rem;
          border: 1px solid #1e293b;
        }
        .sufficiency-score {
          font-size: 0.75rem;
          color: #94a3b8;
          margin-bottom: 0.5rem;
          font-weight: 600;
        }
        .score-val { color: #10b981; }
        .sufficiency-grid {
          display: grid;
          grid-template-columns: 1fr 1fr;
          gap: 0.75rem;
        }
        .aspect-col header {
          font-size: 0.65rem;
          text-transform: uppercase;
          letter-spacing: 0.05em;
          font-weight: 700;
          margin-bottom: 0.4rem;
        }
        .aspect-col.covered header { color: #10b981; }
        .aspect-col.missing header { color: #f59e0b; }
        .aspect-col ul { list-style: none; padding: 0; margin: 0; }
        .aspect-col li {
          font-size: 0.75rem;
          color: #cbd5e1;
          padding: 0.25rem 0;
          border-bottom: 1px solid #1e293b;
        }
        .aspect-col li:last-child { border-bottom: none; }
        
        /* Phase 2 Precision Styles */
        .search-metadata-view {
          margin-top: 0.5rem;
          display: flex;
          flex-direction: column;
          gap: 0.5rem;
        }
        .search-result-item {
          background: #111827;
          border: 1px solid #1e293b;
          border-radius: 0.4rem;
          padding: 0.5rem;
        }
        .search-result-item.conflicting { border-color: #ef4444; }
        .search-result-item header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          margin-bottom: 0.25rem;
        }
        .source-label { font-size: 0.65rem; color: #94a3b8; font-weight: 700; text-transform: uppercase; }
        .score-badge.main { font-size: 0.65rem; color: #10b981; font-weight: 700; }
        .conflict-tag { font-size: 0.6rem; color: white; background: #ef4444; padding: 0.05rem 0.25rem; border-radius: 0.2rem; font-weight: 800; }
        .conflict-reason { font-size: 0.7rem; color: #fca5a5; font-style: italic; margin-bottom: 0.25rem; }
        .metrics { display: flex; gap: 0.5rem; font-size: 0.6rem; color: #64748b; font-weight: 500; }
        
        .event-item.search_started { border-left-color: #3b82f6; }
        .event-item.extraction_started { border-left-color: #f59e0b; }
        .event-item.sufficiency_checked { border-left-color: #10b981; }
        .event-item.grounded_generation_started { border-left-color: #8b5cf6; }
      `}</style>
    </div>
  );
};
