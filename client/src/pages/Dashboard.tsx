import React, { useState, useEffect, useRef } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { Target, Calendar, CheckSquare, Square, Brain, ShieldAlert, Award, FileText, Send, ArrowLeft, RefreshCw } from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

interface Task {
  id: string;
  title: string;
  description: string;
  impact: number;
  urgency: number;
  effort: number;
  alignment: number;
  status: string;
  completed_at?: string;
}

interface Competency {
  area: string;
  progress_percentage: number;
}

interface MemoryItem {
  category: string;
  content: string;
  created_at: string;
}

interface Project {
  id: string;
  title: string;
  target_date: string;
  status: string;
  tasks: Task[];
  competencies: Competency[];
  memory_items: MemoryItem[];
}

interface ChatMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
  reasoning?: string;
  created_at?: string;
}

export const Dashboard: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [project, setProject] = useState<Project | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<'dashboard' | 'chat' | 'review'>('dashboard');
  
  // Reality Gap & Review Reports
  const [realityReport, setRealityReport] = useState('');
  const [loadingReality, setLoadingReality] = useState(false);
  const [reviewReport, setReviewReport] = useState('');
  const [loadingReview, setLoadingReview] = useState(false);

  // Memory Addition Form
  const [memCategory, setMemCategory] = useState('constraint');
  const [memContent, setMemContent] = useState('');
  const [addingMemory, setAddingMemory] = useState(false);

  // Chat Interface State
  const [conversation, setConversation] = useState<any | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [inputValue, setInputValue] = useState('');
  const [chatLoading, setChatLoading] = useState(false);
  const [models, setModels] = useState<any[]>([]);
  const [selectedModel, setSelectedModel] = useState('gemma4:e4b');
  const chatBottomRef = useRef<HTMLDivElement>(null);

  const fetchProject = async () => {
    try {
      const response = await fetch(`/api/projects/get?id=${id}`);
      if (!response.ok) throw new Error('Failed to load project details');
      const data = await response.json();
      setProject(data);
    } catch (err: any) {
      alert(err.message);
      navigate('/projects');
    } finally {
      setLoading(false);
    }
  };

  const fetchModels = async () => {
    try {
      const res = await fetch('/api/models');
      if (res.ok) {
        const data = await res.json();
        setModels(data);
        if (data.length > 0) {
          setSelectedModel(data[0].model_name);
        }
      }
    } catch (err) {}
  };

  const fetchProjectConversation = async () => {
    try {
      // Find standard conversations and locate one belonging to this project
      const response = await fetch('/api/chat/conversations');
      if (response.ok) {
        const conversations = await response.json();
        const linked = conversations.find((c: any) => c.project_id === id);
        if (linked) {
          const detailRes = await fetch(`/api/chat/conversations/get?id=${linked.id}`);
          if (detailRes.ok) {
            const detail = await detailRes.json();
            setConversation(detail);
            setMessages(detail.messages || []);
          }
        }
      }
    } catch (err) {}
  };

  useEffect(() => {
    fetchProject();
    fetchModels();
    fetchProjectConversation();
  }, [id]);

  useEffect(() => {
    if (chatBottomRef.current) {
      chatBottomRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [messages]);

  if (loading || !project) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '80vh' }}>
        <span className="animate-spin" style={{ display: 'inline-block', width: '32px', height: '32px', border: '4px solid #6366f1', borderTopColor: 'transparent', borderRadius: '50%' }} />
      </div>
    );
  }

  // Calculate high leverage priorities
  const activeTasks = project.tasks || [];
  const priorities = [...activeTasks]
    .filter(t => t.status === 'pending')
    .sort((a, b) => {
      // Dynamic priority score based on impact, urgency, and strategic alignment
      const scoreA = a.impact * 2 + a.urgency * 1.5 + a.alignment * 1.5 - a.effort * 0.5;
      const scoreB = b.impact * 2 + b.urgency * 1.5 + b.alignment * 1.5 - b.effort * 0.5;
      return scoreB - scoreA;
    });

  const toggleTaskStatus = async (taskID: string) => {
    const updatedTasks = project.tasks.map(t => {
      if (t.id === taskID) {
        return { ...t, status: t.status === 'completed' ? 'pending' : 'completed' };
      }
      return t;
    });

    try {
      const response = await fetch('/api/projects/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project_id: project.id,
          tasks: updatedTasks
        }),
      });

      if (response.ok) {
        const data = await response.json();
        setProject(data);
      }
    } catch (err) {
      alert('Failed to update task status');
    }
  };

  const handleSliderChange = (idx: number, val: number) => {
    if (!project) return;
    const updated = [...project.competencies];
    updated[idx].progress_percentage = val;
    setProject({ ...project, competencies: updated });
  };

  const saveCompetencies = async () => {
    if (!project) return;
    try {
      const response = await fetch('/api/projects/competencies', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project_id: project.id,
          competencies: project.competencies
        }),
      });
      if (response.ok) {
        const data = await response.json();
        setProject(data);
        alert('Competency progress saved successfully!');
      }
    } catch (err) {
      alert('Failed to save competencies');
    }
  };

  const handleAddMemory = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!memContent) return;

    setAddingMemory(true);
    try {
      const response = await fetch('/api/projects/memory', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          project_id: project.id,
          category: memCategory,
          content: memContent
        }),
      });

      if (response.ok) {
        const data = await response.json();
        setProject(data);
        setMemContent('');
      }
    } catch (err) {
      alert('Failed to save memory item');
    } finally {
      setAddingMemory(false);
    }
  };

  const triggerRealityReport = async () => {
    setLoadingReality(true);
    setRealityReport('');
    try {
      const res = await fetch(`/api/projects/reality-gap?id=${project.id}`);
      if (res.ok) {
        const data = await res.json();
        setRealityReport(data.report);
      } else {
        setRealityReport('Error calling Reality Gap analysis. Ensure your Ollama models are running.');
      }
    } catch (err) {
      setRealityReport('Request failed: ' + err);
    } finally {
      setLoadingReality(false);
    }
  };

  const triggerWeeklyReviewReport = async () => {
    setLoadingReview(true);
    setReviewReport('');
    try {
      const res = await fetch(`/api/projects/weekly-review?id=${project.id}`);
      if (res.ok) {
        const data = await res.json();
        setReviewReport(data.report);
      } else {
        setReviewReport('Error generating executive review. Ensure your Ollama models are running.');
      }
    } catch (err) {
      setReviewReport('Request failed: ' + err);
    } finally {
      setLoadingReview(false);
    }
  };

  const initializeChat = async () => {
    setChatLoading(true);
    try {
      const res = await fetch('/api/chat/conversations/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: `CoS: ${project.title}`,
          project_id: project.id
        })
      });
      if (res.ok) {
        const data = await res.json();
        setConversation(data);
        setMessages([]);
      }
    } catch (err) {
      alert('Failed to initialize chat');
    } finally {
      setChatLoading(false);
    }
  };

  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inputValue || !conversation) return;

    const userPrompt = inputValue;
    setInputValue('');
    setChatLoading(true);

    const newUserMsg: ChatMessage = {
      role: 'user',
      content: userPrompt
    };
    setMessages(prev => [...prev, newUserMsg]);

    const assistantMsg: ChatMessage = {
      role: 'assistant',
      content: '',
      reasoning: ''
    };
    setMessages(prev => [...prev, assistantMsg]);

    try {
      const response = await fetch('/api/chat/completions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          conversation_id: conversation.id,
          model_name: selectedModel,
          content: userPrompt
        })
      });

      if (!response.ok) throw new Error('Stream request failed');

      const reader = response.body?.getReader();
      if (!reader) throw new Error('ReadableStream not supported');

      const decoder = new TextDecoder();
      let buffer = '';
      let isThinking = false;

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
              setChatLoading(false);
              return;
            }

            if (currentEvent === 'thought') {
              setMessages(prev => {
                const copy = [...prev];
                const last = copy[copy.length - 1];
                last.reasoning = (last.reasoning || '') + data;
                return copy;
              });
            } else if (currentEvent === 'error') {
              setMessages(prev => {
                const copy = [...prev];
                copy[copy.length - 1].content = `[Stream Error: ${data}]`;
                return copy;
              });
              setChatLoading(false);
              return;
            } else {
              setMessages(prev => {
                const copy = [...prev];
                const last = copy[copy.length - 1];
                last.content = (last.content || '') + data;
                return copy;
              });
            }
            if (currentEvent !== 'message') currentEvent = 'message';
          }
        }
      }
    } catch (err: any) {
      setMessages(prev => {
        const copy = [...prev];
        copy[copy.length - 1].content = `[Connection Error: ${err.message}]`;
        return copy;
      });
      setChatLoading(false);
    }
  };

  return (
    <div style={{ display: 'grid', gridTemplateRows: 'auto 1fr', height: '100%', overflow: 'hidden' }}>
      
      {/* Workspace Header */}
      <div style={{ padding: '1.25rem 2rem', background: 'white', borderBottom: '1px solid #e2e8f0', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <Link to="/projects" style={{ color: '#64748b', display: 'flex', alignItems: 'center' }}>
            <ArrowLeft size={20} />
          </Link>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#6366f1', fontSize: '0.8rem', fontWeight: 700, textTransform: 'uppercase' }}>
              <Target size={14} /> Goal Workspace
            </div>
            <h2 style={{ fontSize: '1.35rem', fontWeight: 800, color: '#0f172a' }}>{project.title}</h2>
          </div>
        </div>

        {/* Tab Selector */}
        <div style={{ display: 'flex', background: '#f1f5f9', padding: '0.25rem', borderRadius: '0.75rem', gap: '0.125rem' }}>
          <button
            onClick={() => setActiveTab('dashboard')}
            style={{
              padding: '0.5rem 1.25rem',
              borderRadius: '0.5rem',
              border: 'none',
              fontWeight: 600,
              fontSize: '0.875rem',
              cursor: 'pointer',
              background: activeTab === 'dashboard' ? 'white' : 'transparent',
              color: activeTab === 'dashboard' ? '#1e293b' : '#64748b',
              boxShadow: activeTab === 'dashboard' ? '0 1px 3px 0 rgba(0, 0, 0, 0.05)' : 'none',
              transition: 'all 0.2s'
            }}
          >
            Dashboard
          </button>
          <button
            onClick={() => setActiveTab('chat')}
            style={{
              padding: '0.5rem 1.25rem',
              borderRadius: '0.5rem',
              border: 'none',
              fontWeight: 600,
              fontSize: '0.875rem',
              cursor: 'pointer',
              background: activeTab === 'chat' ? 'white' : 'transparent',
              color: activeTab === 'chat' ? '#1e293b' : '#64748b',
              boxShadow: activeTab === 'chat' ? '0 1px 3px 0 rgba(0, 0, 0, 0.05)' : 'none',
              transition: 'all 0.2s'
            }}
          >
            Accountability Chat
          </button>
          <button
            onClick={() => setActiveTab('review')}
            style={{
              padding: '0.5rem 1.25rem',
              borderRadius: '0.5rem',
              border: 'none',
              fontWeight: 600,
              fontSize: '0.875rem',
              cursor: 'pointer',
              background: activeTab === 'review' ? 'white' : 'transparent',
              color: activeTab === 'review' ? '#1e293b' : '#64748b',
              boxShadow: activeTab === 'review' ? '0 1px 3px 0 rgba(0, 0, 0, 0.05)' : 'none',
              transition: 'all 0.2s'
            }}
          >
            Weekly Review
          </button>
        </div>
      </div>

      {/* Main Panel Content */}
      <div style={{ overflowY: 'auto', background: '#f8fafc' }}>
        
        {/* TAB 1: DASHBOARD */}
        {activeTab === 'dashboard' && (
          <div style={{ maxWidth: '1200px', margin: '2rem auto', padding: '0 2rem 4rem', display: 'grid', gridTemplateColumns: '1fr 380px', gap: '2rem' }}>
            
            {/* Left Column: Priorities & Decomposition */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '2rem' }}>
              
              {/* Daily Priorities */}
              <div style={{ background: 'white', borderRadius: '1.25rem', padding: '1.75rem', border: '1px solid #e2e8f0', boxShadow: '0 1px 3px 0 rgba(0,0,0,0.02)' }}>
                <h3 style={{ fontSize: '1.1rem', fontWeight: 750, color: '#0f172a', marginBottom: '1rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <Award size={18} style={{ color: '#f59e0b' }} /> Daily Priorities Center
                </h3>
                
                {priorities.length === 0 ? (
                  <p style={{ color: '#64748b', fontSize: '0.9rem' }}>All milestones completed! Create new tasks below.</p>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                    {priorities.slice(0, 4).map((task) => (
                      <div
                        key={task.id}
                        onClick={() => toggleTaskStatus(task.id)}
                        style={{
                          display: 'flex',
                          alignItems: 'flex-start',
                          gap: '0.75rem',
                          padding: '0.85rem',
                          background: '#f8fafc',
                          border: '1px solid #f1f5f9',
                          borderRadius: '0.75rem',
                          cursor: 'pointer',
                          transition: 'all 0.2s'
                        }}
                        onMouseOver={(e) => e.currentTarget.style.borderColor = '#cbd5e1'}
                        onMouseOut={(e) => e.currentTarget.style.borderColor = '#f1f5f9'}
                      >
                        <div style={{ color: '#6366f1', marginTop: '0.1rem' }}>
                          <Square size={18} />
                        </div>
                        <div>
                          <div style={{ fontWeight: 600, fontSize: '0.95rem', color: '#1e293b' }}>{task.title}</div>
                          <div style={{ fontSize: '0.8rem', color: '#64748b', marginTop: '0.125rem' }}>{task.description}</div>
                          <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.4rem' }}>
                            <span style={{ fontSize: '0.7rem', padding: '0.15rem 0.4rem', borderRadius: '0.25rem', background: '#e0f2fe', color: '#0369a1', fontWeight: 600 }}>Impact: {task.impact}/10</span>
                            <span style={{ fontSize: '0.7rem', padding: '0.15rem 0.4rem', borderRadius: '0.25rem', background: '#fee2e2', color: '#991b1b', fontWeight: 600 }}>Urgency: {task.urgency}/10</span>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* Goal Decomposition Tree */}
              <div style={{ background: 'white', borderRadius: '1.25rem', padding: '1.75rem', border: '1px solid #e2e8f0', boxShadow: '0 1px 3px 0 rgba(0,0,0,0.02)' }}>
                <h3 style={{ fontSize: '1.1rem', fontWeight: 750, color: '#0f172a', marginBottom: '1.25rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <Target size={18} style={{ color: '#6366f1' }} /> Milestone Decomposition Tree
                </h3>

                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                  {activeTasks.map((task) => {
                    const isCompleted = task.status === 'completed';
                    return (
                      <div
                        key={task.id}
                        onClick={() => toggleTaskStatus(task.id)}
                        style={{
                          display: 'flex',
                          alignItems: 'flex-start',
                          gap: '0.75rem',
                          padding: '1rem',
                          border: '1px solid #e2e8f0',
                          borderRadius: '0.75rem',
                          cursor: 'pointer',
                          background: isCompleted ? '#faf5ff' : 'white',
                          opacity: isCompleted ? 0.75 : 1,
                          transition: 'all 0.2s'
                        }}
                      >
                        <div style={{ color: isCompleted ? '#a855f7' : '#94a3b8', marginTop: '0.1rem' }}>
                          {isCompleted ? <CheckSquare size={18} /> : <Square size={18} />}
                        </div>
                        <div style={{ flex: 1 }}>
                          <div style={{ fontWeight: 600, fontSize: '0.95rem', color: '#1e293b', textDecoration: isCompleted ? 'line-through' : 'none' }}>
                            {task.title}
                          </div>
                          <div style={{ fontSize: '0.825rem', color: '#64748b', marginTop: '0.25rem' }}>
                            {task.description}
                          </div>
                        </div>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', alignItems: 'flex-end', fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>
                          <span>Impact: {task.impact}</span>
                          <span>Effort: {task.effort}</span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>

            {/* Right Column: Competencies & Memory Constraints */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '2rem' }}>
              
              {/* Competency Slider progress */}
              <div style={{ background: 'white', borderRadius: '1.25rem', padding: '1.75rem', border: '1px solid #e2e8f0' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem' }}>
                  <h3 style={{ fontSize: '1rem', fontWeight: 750, color: '#0f172a', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                    <Award size={18} style={{ color: '#10b981' }} /> Progress Intelligence
                  </h3>
                  <button
                    onClick={saveCompetencies}
                    style={{ background: 'transparent', border: 'none', color: '#6366f1', fontWeight: 700, fontSize: '0.8rem', cursor: 'pointer' }}
                  >
                    Save Changes
                  </button>
                </div>

                <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                  {project.competencies && project.competencies.map((comp, idx) => (
                    <div key={idx}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.825rem', fontWeight: 600, color: '#475569', marginBottom: '0.375rem' }}>
                        <span>{comp.area}</span>
                        <span>{comp.progress_percentage}%</span>
                      </div>
                      <input
                        type="range"
                        min="0"
                        max="100"
                        value={comp.progress_percentage}
                        onChange={(e) => handleSliderChange(idx, parseInt(e.target.value))}
                        style={{ width: '100%', height: '4px', background: '#e2e8f0', borderRadius: '2px', cursor: 'pointer' }}
                      />
                    </div>
                  ))}
                </div>
              </div>

              {/* Reality Gap Detector Widget */}
              <div style={{ background: 'linear-gradient(135deg, #1e1b4b, #311042)', color: 'white', borderRadius: '1.25rem', padding: '1.75rem' }}>
                <h3 style={{ fontSize: '1rem', fontWeight: 750, marginBottom: '0.75rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <ShieldAlert size={18} style={{ color: '#f43f5e' }} /> Reality Gap Detection
                </h3>
                <p style={{ fontSize: '0.8rem', color: '#c7d2fe', marginBottom: '1.25rem', lineHeight: 1.4 }}>
                  Compare target date readiness against competency curves and time allocations.
                </p>

                <button
                  onClick={triggerRealityReport}
                  disabled={loadingReality}
                  style={{
                    background: '#f43f5e',
                    color: 'white',
                    border: 'none',
                    padding: '0.6rem 1rem',
                    borderRadius: '0.5rem',
                    fontWeight: 600,
                    fontSize: '0.85rem',
                    cursor: 'pointer',
                    width: '100%',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: '0.5rem'
                  }}
                >
                  {loadingReality ? (
                    <>
                      <RefreshCw size={14} className="animate-spin" /> Analyzing Readiness...
                    </>
                  ) : (
                    'Audit Goal Feasibility'
                  )}
                </button>

                {realityReport && (
                  <div style={{ marginTop: '1.25rem', background: 'rgba(255, 255, 255, 0.08)', borderRadius: '0.75rem', padding: '1rem', fontSize: '0.825rem', border: '1px solid rgba(255,255,255,0.1)', overflowY: 'auto', maxHeight: '250px', color: '#e0e7ff' }}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{realityReport}</ReactMarkdown>
                  </div>
                )}
              </div>

              {/* Long Term Memory Logger */}
              <div style={{ background: 'white', borderRadius: '1.25rem', padding: '1.75rem', border: '1px solid #e2e8f0' }}>
                <h3 style={{ fontSize: '1rem', fontWeight: 750, color: '#0f172a', marginBottom: '1rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <Brain size={18} style={{ color: '#a855f7' }} /> Long-Term Memory (Decisions & Constraints)
                </h3>

                <form onSubmit={handleAddMemory} style={{ display: 'flex', gap: '0.5rem', marginBottom: '1.25rem' }}>
                  <select
                    value={memCategory}
                    onChange={(e) => setMemCategory(e.target.value)}
                    style={{ padding: '0.4rem', border: '1px solid #e2e8f0', borderRadius: '0.5rem', outline: 'none', fontSize: '0.8rem', background: '#f8fafc' }}
                  >
                    <option value="constraint">Constraint</option>
                    <option value="decision">Decision</option>
                    <option value="lesson">Lesson</option>
                  </select>
                  <input
                    type="text"
                    placeholder="Log constraint..."
                    value={memContent}
                    onChange={(e) => setMemContent(e.target.value)}
                    style={{ flex: 1, padding: '0.4rem 0.75rem', border: '1px solid #e2e8f0', borderRadius: '0.5rem', outline: 'none', fontSize: '0.8rem' }}
                  />
                  <button
                    type="submit"
                    disabled={addingMemory || !memContent}
                    style={{ background: '#a855f7', color: 'white', border: 'none', padding: '0.4rem 0.8rem', borderRadius: '0.5rem', fontWeight: 600, fontSize: '0.8rem', cursor: 'pointer' }}
                  >
                    Log
                  </button>
                </form>

                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', maxHeight: '200px', overflowY: 'auto' }}>
                  {project.memory_items && project.memory_items.map((item, idx) => (
                    <div key={idx} style={{ padding: '0.6rem', background: '#f8fafc', borderRadius: '0.5rem', borderLeft: `3px solid ${item.category === 'constraint' ? '#f43f5e' : item.category === 'decision' ? '#3b82f6' : '#10b981'}`, fontSize: '0.8rem' }}>
                      <div style={{ fontWeight: 700, fontSize: '0.7rem', textTransform: 'uppercase', color: '#64748b', marginBottom: '0.15rem' }}>{item.category}</div>
                      <div style={{ color: '#334155' }}>{item.content}</div>
                    </div>
                  ))}
                </div>
              </div>

            </div>
          </div>
        )}

        {/* TAB 2: ACCOUNTABILITY CHAT */}
        {activeTab === 'chat' && (
          <div style={{ height: 'calc(100vh - 135px)', display: 'grid', gridTemplateRows: '1fr auto', overflow: 'hidden' }}>
            
            {/* Message Area */}
            <div style={{ overflowY: 'auto', padding: '2rem' }}>
              {!conversation ? (
                <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'center', height: '100%', gap: '1rem', color: '#64748b' }}>
                  <Brain size={48} style={{ color: '#cbd5e1' }} />
                  <p style={{ fontWeight: 600, color: '#475569' }}>Goal Coach Session is Uninitialized</p>
                  <button
                    onClick={initializeChat}
                    disabled={chatLoading}
                    style={{ background: '#6366f1', color: 'white', border: 'none', padding: '0.75rem 1.5rem', borderRadius: '0.75rem', fontWeight: 600, cursor: 'pointer' }}
                  >
                    Start Accountability Chat
                  </button>
                </div>
              ) : messages.length === 0 ? (
                <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'center', height: '100%', color: '#64748b', textAlign: 'center' }}>
                  <Target size={36} style={{ color: '#cbd5e1', marginBottom: '0.5rem' }} />
                  <p style={{ fontWeight: 600 }}>Conversation initialized!</p>
                  <p style={{ fontSize: '0.8rem' }}>Ask your Chief of Staff AI for advice, or request guidance on today's priorities.</p>
                </div>
              ) : (
                <div style={{ maxWidth: '800px', margin: '0 auto', display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
                  {messages.map((msg, idx) => (
                    <div key={idx} style={{ display: 'flex', flexDirection: 'column', alignSelf: msg.role === 'user' ? 'flex-end' : 'flex-start', maxWidth: '80%', gap: '0.25rem' }}>
                      <div style={{ fontSize: '0.7rem', fontWeight: 600, color: '#64748b', alignSelf: msg.role === 'user' ? 'flex-end' : 'flex-start', textTransform: 'uppercase' }}>
                        {msg.role === 'user' ? 'You' : 'Chief of Staff AI'}
                      </div>
                      
                      {/* Thought block for assistant reasoning */}
                      {msg.reasoning && (
                        <div style={{ background: '#f1f5f9', borderLeft: '3px solid #cbd5e1', padding: '0.75rem', borderRadius: '0.5rem', fontSize: '0.8rem', color: '#475569', marginBottom: '0.5rem', fontStyle: 'italic' }}>
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.reasoning}</ReactMarkdown>
                        </div>
                      )}

                      <div
                        style={{
                          background: msg.role === 'user' ? '#6366f1' : 'white',
                          color: msg.role === 'user' ? 'white' : '#1e293b',
                          padding: '1rem',
                          borderRadius: msg.role === 'user' ? '1rem 1rem 0 1rem' : '1rem 1rem 1rem 0',
                          border: msg.role === 'user' ? 'none' : '1px solid #e2e8f0',
                          boxShadow: '0 1px 2px 0 rgba(0,0,0,0.05)',
                          fontSize: '0.95rem',
                          lineHeight: 1.5
                        }}
                      >
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                      </div>
                    </div>
                  ))}
                  <div ref={chatBottomRef} />
                </div>
              )}
            </div>

            {/* Input Bar */}
            {conversation && (
              <div style={{ background: 'white', borderTop: '1px solid #e2e8f0', padding: '1.25rem 2rem' }}>
                <form onSubmit={handleSendMessage} style={{ maxWidth: '800px', margin: '0 auto', display: 'flex', gap: '1rem', alignItems: 'center' }}>
                  
                  {/* Model Selector in chat */}
                  <select
                    value={selectedModel}
                    onChange={(e) => setSelectedModel(e.target.value)}
                    style={{ padding: '0.6rem', border: '1px solid #e2e8f0', borderRadius: '0.75rem', background: '#f8fafc', fontSize: '0.8rem', outline: 'none' }}
                  >
                    {models.map((m, i) => (
                      <option key={i} value={m.model_name}>{m.model_name}</option>
                    ))}
                  </select>

                  <input
                    type="text"
                    placeholder="Ask how to unblock your goals..."
                    value={inputValue}
                    onChange={(e) => setInputValue(e.target.value)}
                    disabled={chatLoading}
                    style={{ flex: 1, padding: '0.75rem 1rem', border: '1px solid #e2e8f0', borderRadius: '0.75rem', outline: 'none' }}
                  />
                  <button
                    type="submit"
                    disabled={chatLoading || !inputValue}
                    style={{
                      background: '#6366f1',
                      color: 'white',
                      border: 'none',
                      padding: '0.75rem',
                      borderRadius: '50%',
                      cursor: 'pointer',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      opacity: (chatLoading || !inputValue) ? 0.7 : 1
                    }}
                  >
                    <Send size={18} />
                  </button>
                </form>
              </div>
            )}
          </div>
        )}

        {/* TAB 3: WEEKLY REVIEW */}
        {activeTab === 'review' && (
          <div style={{ maxWidth: '800px', margin: '2rem auto', padding: '0 2rem 4rem' }}>
            <div style={{ background: 'white', border: '1px solid #e2e8f0', borderRadius: '1.25rem', padding: '2.5rem', boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.05)' }}>
              
              <div style={{ textAlign: 'center', marginBottom: '2rem' }}>
                <FileText size={48} style={{ color: '#6366f1', margin: '0 auto 1rem' }} />
                <h3 style={{ fontSize: '1.5rem', fontWeight: 800, color: '#0f172a', marginBottom: '0.5rem' }}>Weekly Executive Review</h3>
                <p style={{ color: '#64748b', fontSize: '0.9rem' }}>
                  Analyze completion rates, strategic alignment, bottleneck risks, and plan next week's adjustments.
                </p>
              </div>

              <button
                onClick={triggerWeeklyReviewReport}
                disabled={loadingReview}
                style={{
                  background: '#6366f1',
                  color: 'white',
                  border: 'none',
                  padding: '0.8rem 1.5rem',
                  borderRadius: '0.75rem',
                  fontWeight: 600,
                  cursor: 'pointer',
                  width: '100%',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  gap: '0.5rem',
                  marginBottom: '2rem'
                }}
              >
                {loadingReview ? (
                  <>
                    <RefreshCw size={16} className="animate-spin" /> Compiling Weekly Analytics...
                  </>
                ) : (
                  'Generate Weekly Executive Assessment'
                )}
              </button>

              {reviewReport && (
                <div style={{ borderTop: '1px solid #e2e8f0', paddingTop: '2rem', color: '#334155', lineHeight: 1.6 }} className="review-markdown">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{reviewReport}</ReactMarkdown>
                </div>
              )}

            </div>
          </div>
        )}

      </div>
    </div>
  );
};
