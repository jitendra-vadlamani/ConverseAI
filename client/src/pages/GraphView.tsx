import React, { useState, useEffect, useRef } from 'react';
import { 
  Network, Lock, Award, MessageSquare, Send, 
  RefreshCw, HelpCircle, Sparkles, RotateCcw, 
  CheckCircle2, AlertTriangle, BookOpen, ChevronRight,
  BookMarked, HelpCircle as HelpIcon, History, ChevronDown, ChevronUp,
  Maximize2, X
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { 
  ReactFlow, 
  MiniMap, 
  Controls, 
  Background, 
  useNodesState, 
  useEdgesState,
  MarkerType,
  Handle,
  Position
} from '@xyflow/react';
import type { NodeProps } from '@xyflow/react';
import '@xyflow/react/dist/style.css';

interface Progress {
  topic_id: string;
  mastery_score: number;
  last_reviewed?: string;
  notes: string;
}

interface Topic {
  id: string;
  name: string;
  level: number;
  description: string;
  artifact_type?: string;
  created_at: string;
  updated_at: string;
}

interface TopicDetail {
  id: string;
  name: string;
  level: number;
  description: string;
  artifact_type?: string;
  progress?: Progress;
  prerequisites: Topic[];
  locked: boolean;
}

interface TopicEdge {
  from_id: string;
  to_id: string;
  edge_type: string;
}

interface ChatMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
}

interface QuizQuestion {
  question: string;
  options: string[];
  correct_index: number;
  user_selected?: number;
}

interface QuizAttempt {
  id: number;
  topic_id: string;
  score: number;
  questions_json: string;
  created_at: string;
}

// Custom Node Component matching the roadmap.sh design
const TopicNodeComponent = ({ data, selected }: NodeProps & { selected?: boolean }) => {
  const node = data.node as TopicDetail;
  const mastery = node.progress?.mastery_score || 0;
  const isLocked = node.locked;
  const isRoot = node.level === 0 || !node.artifact_type;

  let bg = '#ffffff';
  let border = '1.5px solid #0f172a';
  let color = '#0f172a';
  let shadow = '3px 3px 0px #0f172a'; // Solid retro shadow

  if (isLocked) {
    bg = '#f8fafc';
    border = '1.5px solid #cbd5e1';
    color = '#94a3b8';
    shadow = 'none';
  } else if (isRoot) {
    bg = '#facc15'; // Category header: bright yellow
    border = '2px solid #0f172a';
    shadow = '4px 4px 0px #0f172a';
  } else if (mastery >= 70) {
    bg = '#d1fae5'; // Mastered: light mint
  } else if (mastery > 0) {
    bg = '#fffbeb'; // In progress: warm cream
  }

  return (
    <div style={{ position: 'relative' }}>
      {/* Handles for step connection edges */}
      <Handle type="target" position={Position.Top} style={{ background: '#0f172a', width: '6px', height: '6px' }} />
      
      <div style={{
        width: '160px',
        padding: '10px 12px',
        borderRadius: '6px',
        backgroundColor: bg,
        border: selected ? '2px solid var(--primary)' : border,
        color: color,
        boxShadow: selected ? '0 0 10px rgba(99, 102, 241, 0.4)' : shadow,
        transition: 'all 0.15s ease',
        cursor: 'grab',
        display: 'flex',
        flexDirection: 'column',
        gap: '4px',
        alignItems: 'center',
        justifyContent: 'center',
        textAlign: 'center',
        boxSizing: 'border-box'
      }}>
        {/* Topic Name */}
        <span style={{ 
          fontSize: '11px', 
          fontWeight: isRoot ? '700' : '600',
          lineHeight: '1.3',
          maxWidth: '100%',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          display: '-webkit-box',
          WebkitLineClamp: 2,
          WebkitBoxOrient: 'vertical'
        }}>
          {node.name}
        </span>

        {/* Lock or Checkmark Overlay on the left */}
        {isLocked ? (
          <span style={{
            position: 'absolute',
            left: '-8px',
            top: '50%',
            transform: 'translateY(-50%)',
            width: '16px',
            height: '16px',
            borderRadius: '50%',
            backgroundColor: '#cbd5e1',
            color: 'white',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            border: '1.5px solid #cbd5e1',
            zIndex: 10
          }}>
            <Lock size={8} style={{ color: '#64748b' }} />
          </span>
        ) : mastery >= 70 ? (
          <span style={{
            position: 'absolute',
            left: '-8px',
            top: '50%',
            transform: 'translateY(-50%)',
            width: '16px',
            height: '16px',
            borderRadius: '50%',
            backgroundColor: '#8b5cf6', // Purple checkmark circle
            color: 'white',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: '9px',
            fontWeight: 'bold',
            border: '1.5px solid #0f172a',
            zIndex: 10
          }}>
            ✓
          </span>
        ) : null}

        {/* Mastery Badge floating on top right */}
        {!isRoot && !isLocked && (
          <span style={{
            position: 'absolute',
            right: '-8px',
            top: '-8px',
            padding: '2px 5px',
            borderRadius: '4px',
            backgroundColor: mastery >= 70 ? 'var(--success)' : 'var(--warning)',
            color: 'white',
            fontSize: '8px',
            fontWeight: 'bold',
            border: '1.5px solid #0f172a',
            zIndex: 10
          }}>
            {mastery}%
          </span>
        )}
      </div>

      <Handle type="source" position={Position.Bottom} style={{ background: '#0f172a', width: '6px', height: '6px' }} />
    </div>
  );
};

const nodeTypes = {
  topicNode: TopicNodeComponent
};

export const GraphView: React.FC = () => {
  const [fullGraph, setFullGraph] = useState<{ nodes: TopicDetail[]; edges: TopicEdge[] }>({ nodes: [], edges: [] });
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [nodeDetail, setNodeDetail] = useState<TopicDetail | null>(null);
  const [relations, setRelations] = useState<TopicEdge[]>([]);
  
  // React Flow hooks
  const [nodes, setNodes, onNodesChange] = useNodesState<any>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<any>([]);

  // Drawer Tabs: 'learn' | 'quiz'
  const [activeTab, setActiveTab] = useState<'learn' | 'quiz'>('learn');

  // Study Notes states
  const [studyNotes, setStudyNotes] = useState<string>('');
  const [notesLoading, setNotesLoading] = useState(false);
  const [notesError, setNotesError] = useState<string | null>(null);
  const [isNotesFullscreen, setIsNotesFullscreen] = useState(false);

  // Scoped chat states
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([]);
  const [chatInput, setChatInput] = useState('');
  const [chatLoading, setChatLoading] = useState(false);
  const chatEndRef = useRef<HTMLDivElement>(null);
  const modalChatEndRef = useRef<HTMLDivElement>(null);

  // Planner / Add Content Chat states
  const [plannerMessages, setPlannerMessages] = useState<ChatMessage[]>([]);
  const [plannerInput, setPlannerInput] = useState('');
  const [plannerLoading, setPlannerLoading] = useState(false);
  const plannerEndRef = useRef<HTMLDivElement>(null);

  // Quiz states
  const [quizQuestions, setQuizQuestions] = useState<QuizQuestion[]>([]);
  const [selectedAnswers, setSelectedAnswers] = useState<number[]>([]);
  const [quizState, setQuizState] = useState<'idle' | 'loading' | 'active' | 'result'>('idle');
  const [quizScore, setQuizScore] = useState<number>(0);
  const [quizLoadingError, setQuizLoadingError] = useState<string | null>(null);
  const [submittingQuiz, setSubmittingQuiz] = useState(false);

  // Quiz Revision history states
  const [quizAttempts, setQuizAttempts] = useState<QuizAttempt[]>([]);
  const [attemptsLoading, setAttemptsLoading] = useState(false);
  const [selectedAttemptId, setSelectedAttemptId] = useState<number | null>(null);

  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>(() => {
    try {
      const stored = localStorage.getItem('converseai_node_positions_v3');
      return stored ? JSON.parse(stored) : {};
    } catch {
      return {};
    }
  });

  // Save positions when they change
  useEffect(() => {
    localStorage.setItem('converseai_node_positions_v3', JSON.stringify(nodePositions));
  }, [nodePositions]);

  // Fetch full graph on mount
  useEffect(() => {
    fetchFullGraph();
  }, []);

  // Fetch node detail, relations, notes, and quiz history if selectedNodeId changes
  useEffect(() => {
    if (selectedNodeId) {
      fetchNodeDetail(selectedNodeId);
      fetchRelations(selectedNodeId);
      fetchStudyNotes(selectedNodeId);
      fetchQuizAttempts(selectedNodeId);
      fetchChatHistory(selectedNodeId);
      // Reset Quiz Panel states when switching topics
      setActiveTab('learn');
      setQuizState('idle');
      setQuizQuestions([]);
      setSelectedAnswers([]);
      setQuizScore(0);
      setQuizLoadingError(null);
      setSelectedAttemptId(null);
      setIsNotesFullscreen(false);
    } else {
      setNodeDetail(null);
      setRelations([]);
      setIsNotesFullscreen(false);
    }
  }, [selectedNodeId]);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    modalChatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [chatMessages]);

  useEffect(() => {
    plannerEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [plannerMessages]);

  const fetchFullGraph = async () => {
    try {
      const res = await fetch('/api/topics/all_graph');
      if (res.ok) {
        const data = await res.json();
        setFullGraph(data || { nodes: [], edges: [] });
        
        if (data && data.nodes) {
          const stored = localStorage.getItem('converseai_node_positions_v3');
          const storedPositions = stored ? JSON.parse(stored) : {};
          
          const hasAll = data.nodes.every((n: any) => storedPositions[n.id]);
          if (!hasAll) {
            // Run tidy tree layout calculation
            runLayoutCalculation(data);
          } else {
            setNodePositions(storedPositions);
          }
        }
      }
    } catch (err) {
      console.error('Error fetching full graph:', err);
    }
  };

  const fetchNodeDetail = async (id: string) => {
    try {
      const res = await fetch(`/api/topics/get?id=${id}`);
      if (res.ok) {
        const data = await res.json();
        setNodeDetail(data);
      }
    } catch (err) {
      console.error('Error fetching node detail:', err);
    }
  };

  const fetchRelations = async (id: string) => {
    try {
      const res = await fetch(`/api/topics/relations?id=${id}`);
      if (res.ok) {
        const data = await res.json();
        setRelations(data || []);
      }
    } catch (err) {
      console.error('Error fetching relations:', err);
    }
  };

  // Fetch AI generated Study Notes
  const fetchStudyNotes = async (id: string) => {
    setNotesLoading(true);
    setNotesError(null);
    setStudyNotes('');
    try {
      const res = await fetch(`/api/topics/notes?id=${id}`);
      if (res.ok) {
        const data = await res.json();
        setStudyNotes(data.notes || 'No study notes generated.');
      } else {
        throw new Error("Failed to generate study notes.");
      }
    } catch (err: any) {
      console.error("Notes error:", err);
      setNotesError(err.message || "Failed to load study notes.");
    } finally {
      setNotesLoading(false);
    }
  };

  // Fetch stored chat history for the topic
  const fetchChatHistory = async (id: string) => {
    try {
      const res = await fetch(`/api/topics/chat?id=${id}`);
      if (res.ok) {
        const data = await res.json();
        setChatMessages(data || []);
      }
    } catch (err) {
      console.error("Failed to fetch chat history:", err);
    }
  };

  // Fetch past quiz attempts
  const fetchQuizAttempts = async (id: string) => {
    setAttemptsLoading(true);
    try {
      const res = await fetch(`/api/topics/quiz/attempts?id=${id}`);
      if (res.ok) {
        const data = await res.json();
        setQuizAttempts(data || []);
      }
    } catch (err) {
      console.error("Failed to fetch attempts:", err);
    } finally {
      setAttemptsLoading(false);
    }
  };

  // Scoped chat message send
  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!chatInput.trim() || !selectedNodeId || chatLoading) return;

    const userMsg: ChatMessage = { role: 'user', content: chatInput.trim() };
    const newMessages = [...chatMessages, userMsg];
    setChatMessages(newMessages);
    setChatInput('');
    setChatLoading(true);

    try {
      const res = await fetch(`/api/topics/chat?id=${selectedNodeId}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ messages: newMessages }),
      });
      if (res.ok) {
        const data = await res.json();
        setChatMessages([...newMessages, { role: 'assistant', content: data.response }]);
      }
    } catch (err) {
      console.error('Chat error:', err);
    } finally {
      setChatLoading(false);
    }
  };

  // Add Content / Planner Chat send message
  const handleSendPlannerMessage = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!plannerInput.trim() || plannerLoading) return;

    const userMsg: ChatMessage = { role: 'user', content: plannerInput.trim() };
    const newMessages = [...plannerMessages, userMsg];
    setPlannerMessages(newMessages);
    setPlannerInput('');
    setPlannerLoading(true);

    try {
      const res = await fetch('/api/topics/plan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ messages: newMessages }),
      });
      if (res.ok) {
        const data = await res.json();
        setPlannerMessages([...newMessages, { role: 'assistant', content: data.response }]);
        if (data.graph_updated) {
          fetchFullGraph();
        }
      }
    } catch (err) {
      console.error('Planner error:', err);
    } finally {
      setPlannerLoading(false);
    }
  };

  // Generate customized AI quiz questions (dynamic count)
  const handleStartQuiz = async () => {
    if (!selectedNodeId) return;
    setQuizState('loading');
    setQuizLoadingError(null);
    try {
      const res = await fetch(`/api/topics/quiz/generate?id=${selectedNodeId}`);
      if (res.ok) {
        const data = await res.json();
        if (Array.isArray(data) && data.length > 0) {
          setQuizQuestions(data);
          setSelectedAnswers(new Array(data.length).fill(-1));
          setQuizState('active');
        } else {
          throw new Error("Invalid quiz formatting received from AI.");
        }
      } else {
        throw new Error("Failed to load quiz questions.");
      }
    } catch (err: any) {
      console.error(err);
      setQuizLoadingError(err.message || "Failed to load quiz");
      setQuizState('idle');
    }
  };

  // Evaluate quiz score and update progress on backend
  const handleSubmitQuiz = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedNodeId || quizQuestions.length === 0 || submittingQuiz) return;

    setSubmittingQuiz(true);
    let correctCount = 0;
    
    // Package detailed selection info for future revision
    const detailedQuestions = quizQuestions.map((q, idx) => ({
      ...q,
      user_selected: selectedAnswers[idx]
    }));

    quizQuestions.forEach((q, idx) => {
      if (selectedAnswers[idx] === q.correct_index) {
        correctCount++;
      }
    });

    const score = Math.round((correctCount / quizQuestions.length) * 100);
    setQuizScore(score);

    try {
      const res = await fetch('/api/topics/quiz/submit', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          topic_id: selectedNodeId,
          score: score,
          questions_json: JSON.stringify(detailedQuestions)
        }),
      });
      if (res.ok) {
        // Refresh detail, attempts history, and graph
        fetchNodeDetail(selectedNodeId);
        fetchFullGraph();
        fetchQuizAttempts(selectedNodeId);
        setQuizState('result');
      }
    } catch (err) {
      console.error("Failed to submit quiz:", err);
    } finally {
      setSubmittingQuiz(false);
    }
  };

  const handleBackToQuizHome = () => {
    setQuizState('idle');
    setQuizQuestions([]);
    setSelectedAnswers([]);
    setQuizScore(0);
    setQuizLoadingError(null);
  };

  const handleSelectAnswer = (qIdx: number, oIdx: number) => {
    setSelectedAnswers(prev => {
      const updated = [...prev];
      updated[qIdx] = oIdx;
      return updated;
    });
  };

  const formatDate = (dateStr: string) => {
    try {
      const d = new Date(dateStr);
      return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
    } catch {
      return dateStr;
    }
  };

  // Helper to compute subtree widths (in columns)
  const computeWidth = (nodeId: string, childrenMap: Record<string, string[]>, memo: Record<string, number>): number => {
    const children = childrenMap[nodeId] || [];
    if (children.length === 0) {
      memo[nodeId] = 1;
      return 1;
    }
    let w = 0;
    children.forEach(c => {
      w += computeWidth(c, childrenMap, memo);
    });
    memo[nodeId] = w;
    return w;
  };

  // Helper to recursively assign coordinates centered within subtree widths
  const assignCoordinates = (
    nodeId: string,
    xStart: number,
    y: number,
    childrenMap: Record<string, string[]>,
    widths: Record<string, number>,
    positions: Record<string, {x: number, y: number}>,
    colWidth: number,
    rowHeight: number
  ) => {
    const w = widths[nodeId] || 1;
    const children = childrenMap[nodeId] || [];
    
    // Center the node within its subtree columns (offset by half node width 160/2 = 80 for center alignment)
    const x = xStart + (w * colWidth) / 2 - 80;
    positions[nodeId] = { x, y };
    
    let childXStart = xStart;
    children.forEach(c => {
      const childW = widths[c] || 1;
      assignCoordinates(c, childXStart, y + rowHeight, childrenMap, widths, positions, colWidth, rowHeight);
      childXStart += childW * colWidth;
    });
  };

  // Run Tidy Tree Layout and push positions to both state and React Flow
  const runLayoutCalculation = (graphData = fullGraph) => {
    if (!graphData.nodes || graphData.nodes.length === 0) return;

    const childrenMap: Record<string, string[]> = {};
    const hasParent = new Set<string>();
    
    graphData.nodes.forEach(n => {
      childrenMap[n.id] = [];
    });
    
    graphData.edges.forEach(e => {
      if (e.edge_type === 'part_of') {
        if (childrenMap[e.to_id]) {
          childrenMap[e.to_id].push(e.from_id);
          hasParent.add(e.from_id);
        }
      }
    });

    const roots = graphData.nodes.filter(n => !hasParent.has(n.id)).map(n => n.id);

    const widths: Record<string, number> = {};
    roots.forEach(r => {
      computeWidth(r, childrenMap, widths);
    });

    const computedPositions: Record<string, {x: number, y: number}> = {};
    let xStart = 50;
    const colWidth = 200;
    const rowHeight = 150;

    roots.sort().forEach(r => {
      const rWidth = widths[r] || 1;
      assignCoordinates(r, xStart, 60, childrenMap, widths, computedPositions, colWidth, rowHeight);
      xStart += rWidth * colWidth + 120;
    });

    setNodePositions(computedPositions);
    localStorage.setItem('converseai_node_positions_v3', JSON.stringify(computedPositions));

    // Update React Flow nodes state directly
    setNodes(prevNodes => {
      return prevNodes.map(node => {
        const pos = computedPositions[node.id];
        return pos ? { ...node, position: pos } : node;
      });
    });
  };

  // Reset Layout handler
  const handleResetLayout = () => {
    if (window.confirm("Are you sure you want to reset all node coordinates?")) {
      localStorage.removeItem('converseai_node_positions_v3');
      runLayoutCalculation();
    }
  };

  // Sync React Flow nodes and edges ONLY when fullGraph updates
  useEffect(() => {
    if (!fullGraph.nodes || fullGraph.nodes.length === 0) return;

    const flowNodes = fullGraph.nodes.map((node) => {
      const pos = nodePositions[node.id] || { x: 0, y: 0 };
      return {
        id: node.id,
        type: 'topicNode',
        position: pos,
        data: { node },
        draggable: true
      };
    });
    setNodes(flowNodes);

    const flowEdges = fullGraph.edges.map((edge, idx) => {
      const isPartOf = edge.edge_type === 'part_of';
      const isLocked = edge.edge_type === 'prerequisite_of' && 
        (fullGraph.nodes.find(n => n.id === edge.to_id)?.locked || false);
        
      return {
        id: `edge-${idx}`,
        source: edge.from_id,
        target: edge.to_id,
        type: 'smoothstep',
        animated: !isLocked && !isPartOf,
        style: {
          stroke: isPartOf ? '#cbd5e1' : '#3b82f6',
          strokeWidth: isPartOf ? 1.5 : 2.5,
          strokeDasharray: isPartOf ? '4 4' : undefined
        },
        markerEnd: isPartOf ? undefined : {
          type: MarkerType.ArrowClosed,
          color: '#3b82f6',
          width: 14,
          height: 14
        }
      };
    });
    setEdges(flowEdges);
  }, [fullGraph, nodePositions]);

  // Persist updated position when drag ends
  const onNodeDragStop = (event: any, node: any) => {
    setNodePositions(prev => {
      const updated = { ...prev, [node.id]: node.position };
      localStorage.setItem('converseai_node_positions_v3', JSON.stringify(updated));
      return updated;
    });
  };

  const onNodeClickHandler = (event: React.MouseEvent, node: any) => {
    setSelectedNodeId(node.id);
  };

  // Renders detailed questions list inside attempts revision list
  const renderAttemptDetails = (attempt: QuizAttempt) => {
    try {
      const questions: any[] = JSON.parse(attempt.questions_json);
      return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', padding: '0.75rem', background: 'white', border: '1.5px solid #0f172a', borderRadius: '0.35rem', marginTop: '0.5rem', boxShadow: '2px 2px 0px #0f172a' }}>
          {questions.map((q, idx) => {
            const isCorrect = q.user_selected === q.correct_index;
            return (
              <div key={idx} style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem', fontSize: '0.8rem', borderBottom: idx < questions.length - 1 ? '1px solid #e2e8f0' : 'none', paddingBottom: idx < questions.length - 1 ? '0.5rem' : '0' }}>
                <span style={{ fontWeight: 'bold', color: isCorrect ? 'var(--success)' : 'var(--warning)', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                  Question {idx + 1}: {isCorrect ? '✓ Correct' : '✗ Incorrect'}
                </span>
                <p style={{ margin: 0, fontWeight: 600 }}>{q.question}</p>
                <div style={{ paddingLeft: '0.5rem', display: 'flex', flexDirection: 'column', gap: '0.2rem', marginTop: '0.25rem' }}>
                  {q.options.map((opt: string, oIdx: number) => {
                    const wasSelected = q.user_selected === oIdx;
                    const isRight = q.correct_index === oIdx;
                    let color = '#334155';
                    let weight = 'normal';
                    let prefix = '';
                    if (wasSelected) {
                      weight = 'bold';
                      color = isRight ? 'var(--success)' : 'var(--warning)';
                      prefix = '● ';
                    } else if (isRight) {
                      weight = 'bold';
                      color = 'var(--success)';
                    }
                    return (
                      <span key={oIdx} style={{ color, fontWeight: weight, fontSize: '0.75rem' }}>
                        {prefix}{String.fromCharCode(65 + oIdx)}. {opt} {isRight && ' (Correct)'}
                      </span>
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>
      );
    } catch {
      return <span style={{ fontSize: '0.75rem', color: 'var(--warning)' }}>Failed to parse quiz questions.</span>;
    }
  };

  const totalCount = fullGraph.nodes.length;
  const masteredCount = fullGraph.nodes.filter(n => (n.progress?.mastery_score || 0) >= 70).length;
  const progressCount = fullGraph.nodes.filter(n => {
    const score = n.progress?.mastery_score || 0;
    return score > 0 && score < 70;
  }).length;
  const lockedCount = fullGraph.nodes.filter(n => n.locked).length;

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 380px', height: 'calc(100vh - 60px)', width: '100vw', overflow: 'hidden' }}>
      
      {/* LEFT PANEL: React Flow Canvas */}
      <div style={{ position: 'relative', display: 'flex', flexDirection: 'column', backgroundColor: '#fafafa', borderRight: '1px solid var(--border)' }}>
        
        {/* Canvas Toolbar Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1rem 1.5rem', borderBottom: '1px solid var(--border)', backgroundColor: 'white', zIndex: 10 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <Network size={20} style={{ color: 'var(--primary)' }} />
            <h2 style={{ fontSize: '1.1rem', fontWeight: 700, margin: 0, color: '#0f172a' }}>Knowledge Graph Map</h2>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
            <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>
              Drag nodes to reorganize • Click a node to view details & tutor • Scroll/Pinch to zoom
            </span>
            <button onClick={handleResetLayout} className="icon-btn" title="Reset all node positions to default layout" style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', padding: '0.35rem 0.6rem', height: 'auto', borderRadius: '0.35rem' }}>
              <RotateCcw size={14} /> <span style={{ fontSize: '0.75rem', fontWeight: 600 }}>Reset Layout</span>
            </button>
          </div>
        </div>

        {/* Canvas Area */}
        <div style={{ flex: 1, position: 'relative', overflow: 'hidden' }}>
          {totalCount === 0 ? (
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', gap: '1rem' }}>
              <Sparkles size={48} style={{ opacity: 0.3 }} />
              <p style={{ fontWeight: 500 }}>No topics available. Add topics using the Planning Chat first!</p>
            </div>
          ) : (
            <div style={{ width: '100%', height: '100%', backgroundColor: '#f8fafc' }}>
              <ReactFlow
                nodes={nodes}
                edges={edges}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                onNodeClick={onNodeClickHandler}
                onNodeDragStop={onNodeDragStop}
                nodeTypes={nodeTypes}
                fitView
                fitViewOptions={{ padding: 0.2 }}
                minZoom={0.2}
                maxZoom={2}
                defaultMarkerColor="#3b82f6"
              >
                <Controls showInteractive={false} />
                <MiniMap style={{ bottom: 20, right: 20 }} />
                <Background gap={16} size={1} color="#cbd5e1" />
              </ReactFlow>
            </div>
          )}
        </div>
      </div>

      {/* RIGHT PANEL: Sidebar details */}
      <aside style={{ backgroundColor: 'white', display: 'flex', flexDirection: 'column', height: '100%', overflowY: 'auto', borderLeft: '1px solid var(--border)', zIndex: 10 }}>
        
        {nodeDetail ? (
          /* ACTIVE NODE DETAIL DRAWER */
          <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
            
            {/* Header section of active node */}
            <div style={{ padding: '1.2rem', borderBottom: '1px solid var(--border)' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.75rem', marginBottom: '0.5rem' }}>
                <h3 style={{ fontSize: '1.2rem', fontWeight: 800, color: '#0f172a', margin: 0, flex: 1 }}>
                  {nodeDetail.name}
                </h3>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexShrink: 0 }}>
                  {nodeDetail.progress && (
                    <span className="mastery-badge" style={{ margin: 0 }}>Mastery: {nodeDetail.progress.mastery_score}%</span>
                  )}
                  <button 
                    onClick={() => setSelectedNodeId(null)}
                    title="Close Details & Back to Overview"
                    style={{
                      background: 'none',
                      border: 'none',
                      color: 'var(--text-muted)',
                      cursor: 'pointer',
                      padding: '0.2rem',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      borderRadius: '4px',
                      transition: 'background-color 0.15s'
                    }}
                    onMouseEnter={(e) => e.currentTarget.style.backgroundColor = '#f1f5f9'}
                    onMouseLeave={(e) => e.currentTarget.style.backgroundColor = 'transparent'}
                  >
                    <X size={18} />
                  </button>
                </div>
              </div>
              <p style={{ color: 'var(--text-muted)', fontSize: '0.85rem', marginBottom: '0.75rem' }}>Level: {nodeDetail.level}</p>
              
              {nodeDetail.progress && (
                <div className="progress-bar-container">
                  <div className="progress-bar" style={{ width: `${nodeDetail.progress.mastery_score}%` }}></div>
                </div>
              )}
            </div>

            {/* Lock Status (only shown if locked) */}
            {nodeDetail.locked && (
              <div style={{ padding: '1.5rem', borderBottom: '1px solid var(--border)', backgroundColor: 'var(--bg-subtle)' }}>
                <div className="locked-alert" style={{ marginBottom: 0 }}>
                  <Lock size={16} style={{ flexShrink: 0, marginTop: '2px' }} />
                  <div style={{ fontSize: '0.8rem' }}>
                    <strong>Prerequisites Locked</strong>
                    <p style={{ marginTop: '0.15rem' }}>Prerequisites must be mastered (&ge;70%) to unlock parent and child topics:</p>
                    <ul style={{ paddingLeft: '1rem', marginTop: '0.25rem' }}>
                      {nodeDetail.prerequisites.map(p => (
                        <li key={p.id} style={{ cursor: 'pointer', textDecoration: 'underline' }} onClick={() => setSelectedNodeId(p.id)}>
                          {p.name}
                        </li>
                      ))}
                    </ul>
                  </div>
                </div>
              </div>
            )}

            {/* TWO OPTIONS NAVIGATION TABS: 1. Learn Topic, 2. Take Quiz */}
            {!nodeDetail.locked && (
              <div style={{ display: 'flex', borderBottom: '1px solid var(--border)', backgroundColor: '#fafafa' }}>
                <button 
                  onClick={() => setActiveTab('learn')}
                  style={{
                    flex: 1,
                    padding: '0.85rem 1rem',
                    background: activeTab === 'learn' ? 'white' : 'transparent',
                    border: 'none',
                    borderBottom: activeTab === 'learn' ? '2.5px solid var(--primary)' : '2.5px solid transparent',
                    fontSize: '0.85rem',
                    fontWeight: 700,
                    color: activeTab === 'learn' ? 'var(--primary)' : 'var(--text-muted)',
                    cursor: 'pointer',
                    transition: 'all 0.15s ease',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: '0.35rem'
                  }}
                >
                  <MessageSquare size={14} /> 1. Learn Topic
                </button>
                <button 
                  onClick={() => setActiveTab('quiz')}
                  style={{
                    flex: 1,
                    padding: '0.85rem 1rem',
                    background: activeTab === 'quiz' ? 'white' : 'transparent',
                    border: 'none',
                    borderBottom: activeTab === 'quiz' ? '2.5px solid var(--primary)' : '2.5px solid transparent',
                    fontSize: '0.85rem',
                    fontWeight: 700,
                    color: activeTab === 'quiz' ? 'var(--primary)' : 'var(--text-muted)',
                    cursor: 'pointer',
                    transition: 'all 0.15s ease',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: '0.35rem'
                  }}
                >
                  <Award size={14} /> 2. Take Quiz
                </button>
              </div>
            )}

            {/* TAB CONTENT: 1. LEARN TOPIC (Study Notes + AI Tutor Chat) */}
            {!nodeDetail.locked && activeTab === 'learn' && (
              <div style={{ padding: '1.5rem', flex: 1, display: 'flex', flexDirection: 'column', minHeight: '350px', gap: '1.25rem' }}>
                
                {/* AI STUDY NOTES SECTION */}
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <h4 style={{ fontSize: '0.85rem', fontWeight: 700, color: '#0f172a', display: 'flex', alignItems: 'center', gap: '0.35rem', margin: 0 }}>
                      <BookMarked size={16} style={{ color: 'var(--primary)' }} /> Topic Study Notes
                    </h4>
                    {!notesLoading && !notesError && studyNotes && (
                      <button 
                        onClick={() => setIsNotesFullscreen(true)}
                        title="Fullscreen Study Notes"
                        style={{
                          background: 'none',
                          border: '1.5px solid #0f172a',
                          color: '#0f172a',
                          cursor: 'pointer',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          padding: '4px',
                          borderRadius: '4px',
                          backgroundColor: 'white',
                          boxShadow: '1px 1px 0px #0f172a',
                          transition: 'all 0.1s ease'
                        }}
                        onMouseDown={(e) => { e.currentTarget.style.transform = 'translate(0.5px, 0.5px)'; e.currentTarget.style.boxShadow = '0.5px 0.5px 0px #0f172a'; }}
                        onMouseUp={(e) => { e.currentTarget.style.transform = 'none'; e.currentTarget.style.boxShadow = '1px 1px 0px #0f172a'; }}
                      >
                        <Maximize2 size={12} />
                      </button>
                    )}
                  </div>
                  
                  <div style={{ 
                    background: '#f8fafc', 
                    border: '1.5px solid #0f172a', 
                    padding: '1rem', 
                    borderRadius: '0.5rem', 
                    boxShadow: '2px 2px 0px #0f172a',
                    maxHeight: '260px',
                    overflowY: 'auto',
                    fontSize: '0.85rem',
                    lineHeight: '1.5',
                    color: '#334155'
                  }}>
                    {notesLoading ? (
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', padding: '0.5rem' }}>
                        <RefreshCw size={14} className="spinning" />
                        <span>Generating custom study notes...</span>
                      </div>
                    ) : notesError ? (
                      <span style={{ color: 'var(--warning)' }}>{notesError}</span>
                    ) : (
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{studyNotes}</ReactMarkdown>
                    )}
                  </div>
                </div>

                {/* AI DISCUSSION / CHAT SECTION */}
                <div style={{ display: 'flex', flexDirection: 'column', flex: 1, gap: '0.5rem' }}>
                  <h4 style={{ fontSize: '0.85rem', fontWeight: 700, color: '#0f172a', display: 'flex', alignItems: 'center', gap: '0.35rem', margin: 0 }}>
                    <HelpIcon size={16} style={{ color: 'var(--primary)' }} /> Ask Questions & Clarify Doubts
                  </h4>
                  
                  <div className="chat-messages" style={{ height: '180px', marginBottom: '0.5rem' }}>
                    {chatMessages.length === 0 ? (
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', gap: '0.5rem', padding: '0.5rem', textAlign: 'center' }}>
                        <HelpCircle size={20} style={{ opacity: 0.6 }} />
                        <p style={{ fontSize: '0.75rem' }}>Have doubts? Ask our AI Tutor to clarify any concepts above!</p>
                      </div>
                    ) : (
                      chatMessages.map((m, i) => (
                        <div key={i} className={`message-bubble ${m.role}`}>
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.content}</ReactMarkdown>
                        </div>
                      ))
                    )}
                    {chatLoading && <div className="message-bubble assistant">Thinking...</div>}
                    <div ref={chatEndRef} />
                  </div>
                  <form onSubmit={handleSendMessage} className="chat-input-row">
                    <input type="text" placeholder="Ask a question..." value={chatInput}
                      onChange={(e) => setChatInput(e.target.value)} disabled={chatLoading} style={{ padding: '0.5rem 0.75rem', fontSize: '0.85rem' }} />
                    <button type="submit" className="chat-send-btn" disabled={chatLoading} style={{ padding: '0 0.75rem' }}><Send size={14} /></button>
                  </form>
                </div>

              </div>
            )}

            {/* TAB CONTENT: 2. TAKE QUIZ (Interactive MCQs + Attempts Revision history) */}
            {!nodeDetail.locked && activeTab === 'quiz' && (
              <div style={{ padding: '1.5rem', flex: 1, display: 'flex', flexDirection: 'column', minHeight: '320px', gap: '1.25rem' }}>
                
                {quizState === 'idle' && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
                    
                    {/* Welcome quiz card */}
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', textAlign: 'center', gap: '1rem', background: '#f8fafc', border: '1.5px solid #0f172a', padding: '1.5rem', borderRadius: '0.5rem', boxShadow: '3px 3px 0px #0f172a' }}>
                      <Award size={32} style={{ color: 'var(--primary)' }} />
                      <div>
                        <h4 style={{ fontSize: '0.95rem', fontWeight: 700, margin: 0, color: '#0f172a' }}>Assess Your Understanding</h4>
                        <p style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginTop: '0.5rem', lineHeight: '1.5' }}>
                          Generate an assessment to test your topic comprehension. Achieve &ge;70% to master this node.
                        </p>
                      </div>
                      {quizLoadingError && (
                        <div className="locked-alert" style={{ background: '#fef2f2', border: '1px solid #fca5a5', color: '#b91c1c', display: 'flex', gap: '0.5rem', fontSize: '0.8rem', padding: '0.6rem 0.8rem', borderRadius: '0.5rem' }}>
                          <AlertTriangle size={14} style={{ flexShrink: 0 }} />
                          <span>{quizLoadingError}</span>
                        </div>
                      )}
                      <button onClick={handleStartQuiz} className="auth-button" style={{ padding: '0.6rem 1.2rem', display: 'flex', alignItems: 'center', gap: '0.5rem', alignSelf: 'stretch', justifyContent: 'center' }}>
                        <Sparkles size={14} /> Start Topic Quiz
                      </button>
                    </div>

                    {/* QUIZ ATTEMPTS REVISION HISTORY */}
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.50rem' }}>
                      <h4 style={{ fontSize: '0.85rem', fontWeight: 700, color: '#0f172a', display: 'flex', alignItems: 'center', gap: '0.35rem', margin: 0 }}>
                        <History size={16} style={{ color: 'var(--primary)' }} /> Quiz Revision History
                      </h4>

                      {attemptsLoading ? (
                        <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                          <RefreshCw size={12} className="spinning" /> Loading history...
                        </div>
                      ) : quizAttempts.length === 0 ? (
                        <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>No previous quiz results recorded for this topic.</span>
                      ) : (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', maxHeight: '200px', overflowY: 'auto', paddingRight: '0.25rem' }}>
                          {quizAttempts.map((attempt) => {
                            const passed = attempt.score >= 70;
                            const isExpanded = selectedAttemptId === attempt.id;
                            return (
                              <div key={attempt.id} style={{ display: 'flex', flexDirection: 'column' }}>
                                <div 
                                  onClick={() => setSelectedAttemptId(isExpanded ? null : attempt.id)}
                                  style={{
                                    display: 'flex',
                                    justifyContent: 'space-between',
                                    alignItems: 'center',
                                    padding: '0.5rem 0.75rem',
                                    border: '1.5px solid #0f172a',
                                    borderRadius: '0.35rem',
                                    background: passed ? '#d1fae5' : '#fef2f2',
                                    boxShadow: '1.5px 1.5px 0px #0f172a',
                                    cursor: 'pointer',
                                    fontSize: '0.8rem'
                                  }}
                                >
                                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                                    <span style={{ fontWeight: 800 }}>{attempt.score}%</span>
                                    <span style={{ color: 'var(--text-muted)', fontSize: '0.75rem' }}>{formatDate(attempt.created_at)}</span>
                                  </div>
                                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                                    <span style={{ 
                                      fontSize: '9px', 
                                      fontWeight: 'bold', 
                                      padding: '2px 6px', 
                                      borderRadius: '3px',
                                      background: passed ? '#10b981' : '#ef4444',
                                      color: 'white'
                                    }}>
                                      {passed ? 'PASSED' : 'FAILED'}
                                    </span>
                                    {isExpanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
                                  </div>
                                </div>

                                {isExpanded && renderAttemptDetails(attempt)}
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </div>

                  </div>
                )}

                {quizState === 'loading' && (
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', gap: '1rem', textAlign: 'center', padding: '2rem' }}>
                    <RefreshCw size={32} className="spinning" style={{ color: 'var(--primary)' }} />
                    <p style={{ fontSize: '0.85rem', color: 'var(--text-muted)', fontWeight: 600 }}>
                      AI tutor is generating custom quiz questions... please wait.
                    </p>
                  </div>
                )}

                {quizState === 'active' && (
                  <form onSubmit={handleSubmitQuiz} style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', maxHeight: '350px', overflowY: 'auto', paddingRight: '0.25rem' }}>
                      {quizQuestions.map((q, qIdx) => (
                        <div key={qIdx} style={{ background: '#f8fafc', border: '1.5px solid #0f172a', padding: '1rem', borderRadius: '0.5rem', boxShadow: '2px 2px 0px #0f172a', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                          <span style={{ fontSize: '0.8rem', fontWeight: 800, color: 'var(--primary)' }}>QUESTION {qIdx + 1} OF {quizQuestions.length}</span>
                          <p style={{ fontSize: '0.85rem', fontWeight: 700, margin: 0, color: '#0f172a', lineHeight: '1.4' }}>{q.question}</p>
                          
                          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                            {q.options.map((opt, oIdx) => {
                              const isSelected = selectedAnswers[qIdx] === oIdx;
                              return (
                                <button
                                  key={oIdx}
                                  type="button"
                                  onClick={() => handleSelectAnswer(qIdx, oIdx)}
                                  style={{
                                    textAlign: 'left',
                                    padding: '0.6rem 0.8rem',
                                    borderRadius: '0.35rem',
                                    fontSize: '0.8rem',
                                    fontWeight: isSelected ? '700' : '500',
                                    cursor: 'pointer',
                                    background: isSelected ? '#facc15' : 'white',
                                    border: isSelected ? '2px solid #0f172a' : '1.5px solid #e2e8f0',
                                    color: '#0f172a',
                                    boxShadow: isSelected ? '2px 2px 0px #0f172a' : 'none',
                                    transition: 'all 0.15s ease',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: '0.5rem'
                                  }}
                                >
                                  <span style={{ 
                                    width: '18px', 
                                    height: '18px', 
                                    borderRadius: '50%', 
                                    border: '1.5px solid #0f172a', 
                                    display: 'flex', 
                                    alignItems: 'center', 
                                    justifyContent: 'center', 
                                    fontSize: '9px',
                                    fontWeight: 'bold',
                                    background: isSelected ? '#0f172a' : 'white',
                                    color: isSelected ? 'white' : '#0f172a'
                                  }}>
                                    {String.fromCharCode(65 + oIdx)}
                                  </span>
                                  {opt}
                                </button>
                              );
                            })}
                          </div>
                        </div>
                      ))}
                    </div>

                    <button
                      type="submit"
                      className="auth-button"
                      disabled={submittingQuiz || selectedAnswers.some(ans => ans === -1)}
                      style={{ padding: '0.65rem', fontSize: '0.85rem' }}
                    >
                      {submittingQuiz ? 'Submitting Score...' : 'Submit Answers'}
                    </button>
                  </form>
                )}

                {quizState === 'result' && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', padding: '0.25rem', width: '100%', boxSizing: 'border-box' }}>
                    
                    {/* Compact Horizontal Score Banner */}
                    <div style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '0.75rem',
                      background: quizScore >= 70 ? '#d1fae5' : '#fef2f2',
                      border: '2px solid #0f172a',
                      borderRadius: '0.5rem',
                      padding: '0.6rem 0.8rem',
                      boxShadow: '2.5px 2.5px 0px #0f172a',
                      width: '100%',
                      boxSizing: 'border-box'
                    }}>
                      <div style={{
                        width: '42px',
                        height: '42px',
                        borderRadius: '50%',
                        border: '2px solid #0f172a',
                        background: 'white',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        fontWeight: 800,
                        fontSize: '0.95rem',
                        flexShrink: 0
                      }}>
                        {quizScore}%
                      </div>
                      <div style={{ textAlign: 'left' }}>
                        <h4 style={{ fontSize: '0.85rem', fontWeight: 800, color: '#0f172a', margin: 0, display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                          {quizScore >= 70 ? <CheckCircle2 size={13} style={{ color: 'var(--success)' }} /> : <AlertTriangle size={13} style={{ color: 'var(--warning)' }} />}
                          {quizScore >= 70 ? 'Mastery Achieved!' : 'Keep Practicing!'}
                        </h4>
                        <p style={{ fontSize: '0.7rem', color: '#475569', margin: '2px 0 0 0', lineHeight: '1.3' }}>
                          {quizScore >= 70 ? 'You mastered this topic and unlocked dependent paths.' : 'Score \u226570% is required to unlock downstream topics.'}
                        </p>
                      </div>
                    </div>

                    {/* Question Reviews */}
                    <div style={{
                      display: 'flex',
                      flexDirection: 'column',
                      gap: '0.5rem',
                      width: '100%',
                      maxHeight: '230px',
                      overflowY: 'auto',
                      textAlign: 'left',
                      border: '1.5px solid #0f172a',
                      borderRadius: '0.35rem',
                      background: '#f8fafc',
                      padding: '0.6rem',
                      boxSizing: 'border-box'
                    }}>
                      <h5 style={{ fontSize: '0.75rem', fontWeight: 800, color: '#0f172a', margin: '0 0 0.25rem 0', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Review Answers:</h5>
                      {quizQuestions.map((q, idx) => {
                        const userSel = selectedAnswers[idx];
                        const isCorrect = userSel === q.correct_index;
                        return (
                          <div key={idx} style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', fontSize: '0.72rem', borderBottom: idx < quizQuestions.length - 1 ? '1px solid #cbd5e1' : 'none', paddingBottom: idx < quizQuestions.length - 1 ? '0.4rem' : '0' }}>
                            <span style={{ fontWeight: 'bold', color: isCorrect ? 'var(--success)' : '#d97706', display: 'flex', alignItems: 'center', gap: '0.2rem' }}>
                              Q{idx + 1}: {isCorrect ? '✓ Correct' : '✗ Incorrect'}
                            </span>
                            <p style={{ margin: 0, fontWeight: 600, color: '#0f172a' }}>{q.question}</p>
                            <div style={{ paddingLeft: '0.4rem', display: 'flex', flexDirection: 'column', gap: '0.1rem', marginTop: '0.1rem' }}>
                              {q.options.map((opt, oIdx) => {
                                const wasSelected = userSel === oIdx;
                                const isRight = q.correct_index === oIdx;
                                let color = '#475569';
                                let weight = 'normal';
                                let prefix = '';
                                if (wasSelected) {
                                  weight = 'bold';
                                  color = isRight ? 'var(--success)' : 'var(--error)';
                                  prefix = '● ';
                                } else if (isRight) {
                                  weight = 'bold';
                                  color = 'var(--success)';
                                }
                                return (
                                  <span key={oIdx} style={{ color, fontWeight: weight, fontSize: '0.7rem' }}>
                                    {prefix}{String.fromCharCode(65 + oIdx)}. {opt} {isRight && ' (Correct)'}
                                  </span>
                                );
                              })}
                            </div>
                          </div>
                        );
                      })}
                    </div>

                    {/* Action buttons */}
                    <div style={{ display: 'flex', gap: '0.5rem', width: '100%', marginTop: '0.25rem' }}>
                      <button onClick={handleStartQuiz} className="icon-btn" style={{ flex: 1, padding: '0.45rem', background: 'white', border: '1.5px solid #0f172a', borderRadius: '0.35rem', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '0.25rem', cursor: 'pointer', fontWeight: 700, fontSize: '0.75rem', boxShadow: '2px 2px 0px #0f172a', transition: 'all 0.1s' }} onMouseDown={(e) => { e.currentTarget.style.transform = 'translate(1px, 1px)'; e.currentTarget.style.boxShadow = '1px 1px 0px #0f172a'; }} onMouseUp={(e) => { e.currentTarget.style.transform = 'none'; e.currentTarget.style.boxShadow = '2px 2px 0px #0f172a'; }}>
                        <RotateCcw size={12} /> Retake Quiz
                      </button>
                      <button onClick={handleBackToQuizHome} className="icon-btn" style={{ flex: 1, padding: '0.45rem', background: 'white', border: '1.5px solid #0f172a', borderRadius: '0.35rem', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '0.25rem', cursor: 'pointer', fontWeight: 700, fontSize: '0.75rem', boxShadow: '2px 2px 0px #0f172a', transition: 'all 0.1s' }} onMouseDown={(e) => { e.currentTarget.style.transform = 'translate(1px, 1px)'; e.currentTarget.style.boxShadow = '1px 1px 0px #0f172a'; }} onMouseUp={(e) => { e.currentTarget.style.transform = 'none'; e.currentTarget.style.boxShadow = '2px 2px 0px #0f172a'; }}>
                        Back to Home
                      </button>
                    </div>
                  </div>
                )}

              </div>
            )}

            {/* LOCKED INFORMATION DRAWER (if node is locked, no options can be loaded) */}
            {nodeDetail.locked && (
              <div style={{ padding: '2rem 1.5rem', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '1rem', textAlign: 'center', background: '#fafafa', borderTop: '1px solid var(--border)', flex: 1 }}>
                <Lock size={32} style={{ color: '#cbd5e1' }} />
                <div>
                  <h4 style={{ fontSize: '0.9rem', fontWeight: 700, color: '#64748b', margin: 0 }}>Topic is Locked</h4>
                  <p style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginTop: '0.35rem', lineHeight: '1.5' }}>
                    Complete the prerequisites listed above to open this study block and start learning.
                  </p>
                </div>
              </div>
            )}

          </div>
        ) : (
          /* EMPTY SIDEBAR STATE: GRAPH SUMMARY, STATISTICS & ADD CONTENT CHAT */
          <div style={{ padding: '1.5rem 1.25rem', display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
            
            {/* Overview Header */}
            <div>
              <h3 style={{ fontSize: '1.1rem', fontWeight: 800, color: '#0f172a', marginBottom: '0.25rem' }}>Graph Overview</h3>
              <p style={{ fontSize: '0.8rem', color: 'var(--text-muted)', lineHeight: '1.4', margin: 0 }}>
                This map represents all active topics and their connections. Click any node to drill down.
              </p>
            </div>

            {/* ADD CONTENT CHAT SECTION */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', border: '2px solid #0f172a', borderRadius: '0.75rem', padding: '1rem', background: '#fff', boxShadow: '4px 4px 0px #0f172a', boxSizing: 'border-box' }}>
              <h4 style={{ fontSize: '0.85rem', fontWeight: 800, color: '#0f172a', display: 'flex', alignItems: 'center', gap: '0.35rem', margin: 0 }}>
                <Sparkles size={15} style={{ color: 'var(--primary)' }} /> Expand Roadmap AI
              </h4>
              <p style={{ fontSize: '0.72rem', color: 'var(--text-muted)', margin: 0, lineHeight: '1.3' }}>
                Chat with the AI planner to add new nodes, requirements, or categories in real-time.
              </p>

              {/* Chat messages */}
              <div className="chat-messages" style={{ height: '180px', marginBottom: '0.25rem', background: '#f8fafc', border: '1.5px solid #0f172a', borderRadius: '0.35rem', padding: '0.5rem' }}>
                {plannerMessages.length === 0 ? (
                  <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', gap: '0.25rem', textAlign: 'center', padding: '0.25rem' }}>
                    <MessageSquare size={18} style={{ opacity: 0.5 }} />
                    <p style={{ fontSize: '0.72rem', margin: 0 }}>
                      Try: <em>"Add Python programming as a prerequisite to Docker"</em>
                    </p>
                  </div>
                ) : (
                  plannerMessages.map((m, i) => (
                    <div key={i} className={`message-bubble ${m.role}`} style={{ fontSize: '0.75rem', padding: '0.4rem 0.6rem' }}>
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.content}</ReactMarkdown>
                    </div>
                  ))
                )}
                {plannerLoading && <div className="message-bubble assistant" style={{ fontSize: '0.75rem', padding: '0.4rem 0.6rem' }}>Thinking...</div>}
                <div ref={plannerEndRef} />
              </div>

              {/* Chat input */}
              <form onSubmit={handleSendPlannerMessage} className="chat-input-row">
                <input 
                  type="text" 
                  placeholder="Enter a new topic or request..." 
                  value={plannerInput}
                  onChange={(e) => setPlannerInput(e.target.value)} 
                  disabled={plannerLoading} 
                  style={{ padding: '0.5rem 0.75rem', fontSize: '0.8rem' }} 
                />
                <button 
                  type="submit" 
                  className="chat-send-btn" 
                  disabled={plannerLoading} 
                  style={{ padding: '0 0.75rem' }}
                >
                  <Send size={13} />
                </button>
              </form>
            </div>

            {/* Interactive Progress Ring / Stat Indicators */}
            <div style={{ background: '#f8fafc', padding: '1rem', borderRadius: '0.75rem', border: '1.5px solid #0f172a', boxShadow: '2px 2px 0px #0f172a' }}>
              <h4 style={{ fontSize: '0.8rem', fontWeight: 800, color: '#334155', marginBottom: '0.75rem', margin: 0 }}>Study Statistics</h4>
              
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginTop: '0.5rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.75rem' }}>
                  <span style={{ color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                    <BookOpen size={13} /> Total Topics:
                  </span>
                  <strong style={{ color: '#0f172a' }}>{totalCount}</strong>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.75rem' }}>
                  <span style={{ color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                    <CheckCircle2 size={13} style={{ color: 'var(--success)' }} /> Mastered (&ge;70%):
                  </span>
                  <strong style={{ color: 'var(--success)' }}>{masteredCount}</strong>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.75rem' }}>
                  <span style={{ color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                    <Award size={13} style={{ color: 'var(--warning)' }} /> In Progress:
                  </span>
                  <strong style={{ color: 'var(--warning)' }}>{progressCount}</strong>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.75rem' }}>
                  <span style={{ color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                    <Lock size={13} style={{ color: '#94a3b8' }} /> Locked:
                  </span>
                  <strong style={{ color: '#64748b' }}>{lockedCount}</strong>
                </div>
              </div>

              {/* Progress bar representing mastery rate */}
              {totalCount > 0 && (
                <div style={{ marginTop: '0.75rem' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.7rem', fontWeight: 700, marginBottom: '0.25rem' }}>
                    <span>Overall Mastery Progress</span>
                    <span>{Math.round((masteredCount / totalCount) * 100)}%</span>
                  </div>
                  <div className="progress-bar-container" style={{ height: '5px' }}>
                    <div className="progress-bar" style={{ width: `${(masteredCount / totalCount) * 100}%`, backgroundColor: 'var(--success)' }}></div>
                  </div>
                </div>
              )}
            </div>

            {/* Quick Tips */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              <h4 style={{ fontSize: '0.8rem', fontWeight: 800, color: '#334155', margin: 0 }}>Quick Tips</h4>
              <div style={{ display: 'flex', gap: '0.4rem', fontSize: '0.75rem', color: 'var(--text-muted)', lineHeight: '1.3' }}>
                <Sparkles size={13} style={{ color: 'var(--primary)', flexShrink: 0, marginTop: '1px' }} />
                <span>Drag any node to position it exactly how you want.</span>
              </div>
              <div style={{ display: 'flex', gap: '0.4rem', fontSize: '0.75rem', color: 'var(--text-muted)', lineHeight: '1.3' }}>
                <ChevronRight size={13} style={{ color: 'var(--primary)', flexShrink: 0, marginTop: '1px' }} />
                <span>Selecting a node lets you read details, take quizzes, and chat with AI.</span>
              </div>
            </div>

          </div>
        )}
      </aside>

      {/* FULLSCREEN STUDY NOTES & DOUBTS CHAT MODAL */}
      {isNotesFullscreen && (
        <div style={{
          position: 'fixed',
          top: 0,
          left: 0,
          width: '100vw',
          height: '100vh',
          backgroundColor: 'rgba(15, 23, 42, 0.75)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 9999,
          padding: '1.5rem',
          boxSizing: 'border-box',
          backdropFilter: 'blur(4px)'
        }}>
          <style>{`
            .fullscreen-markdown-body h1, .fullscreen-markdown-body h2, .fullscreen-markdown-body h3 {
              color: #0f172a;
              margin-top: 1.5rem;
              margin-bottom: 0.75rem;
              font-weight: 800;
            }
            .fullscreen-markdown-body h1 { font-size: 1.5rem; border-bottom: 3px solid #0f172a; padding-bottom: 0.35rem; }
            .fullscreen-markdown-body h2 { font-size: 1.25rem; }
            .fullscreen-markdown-body h3 { font-size: 1.1rem; }
            .fullscreen-markdown-body p { margin-bottom: 1rem; line-height: 1.6; }
            .fullscreen-markdown-body ul, .fullscreen-markdown-body ol { margin-bottom: 1rem; padding-left: 1.5rem; }
            .fullscreen-markdown-body li { margin-bottom: 0.35rem; }
            .fullscreen-markdown-body code {
              background-color: #0f172a;
              color: #38bdf8;
              padding: 0.15rem 0.35rem;
              border-radius: 0.25rem;
              font-family: monospace;
              font-size: 0.8rem;
            }
            .fullscreen-markdown-body pre {
              background-color: #0f172a;
              padding: 1rem;
              border-radius: 0.5rem;
              overflow-x: auto;
              margin-bottom: 1.25rem;
              border: 2px solid #0f172a;
              box-shadow: 2px 2px 0px #0f172a;
            }
            .fullscreen-markdown-body pre code {
              background-color: transparent;
              color: #f8fafc;
              padding: 0;
              font-size: 0.8rem;
            }
            @keyframes modalSlideIn {
              from { transform: translateY(20px); opacity: 0; }
              to { transform: translateY(0); opacity: 1; }
            }
          `}</style>

          <div style={{
            backgroundColor: 'white',
            border: '4px solid #0f172a',
            borderRadius: '0.75rem',
            width: '100%',
            maxWidth: '1200px',
            height: '92vh',
            display: 'flex',
            flexDirection: 'column',
            boxShadow: '8px 8px 0px #0f172a',
            overflow: 'hidden',
            animation: 'modalSlideIn 0.2s ease-out'
          }}>
            
            {/* Modal Header */}
            <div style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              padding: '1rem 1.5rem',
              borderBottom: '3px solid #0f172a',
              backgroundColor: '#fafafa'
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <BookMarked size={20} style={{ color: 'var(--primary)' }} />
                <h3 style={{ fontSize: '1.15rem', fontWeight: 800, color: '#0f172a', margin: 0 }}>
                  Study Space: {nodeDetail?.name}
                </h3>
              </div>
              <button 
                onClick={() => setIsNotesFullscreen(false)}
                style={{
                  padding: '0.35rem 0.75rem',
                  backgroundColor: 'white',
                  border: '2px solid #0f172a',
                  borderRadius: '0.35rem',
                  fontWeight: 700,
                  cursor: 'pointer',
                  boxShadow: '2px 2px 0px #0f172a',
                  transition: 'all 0.15s ease'
                }}
                onMouseDown={(e) => { e.currentTarget.style.transform = 'translate(1px, 1px)'; e.currentTarget.style.boxShadow = '1px 1px 0px #0f172a'; }}
                onMouseUp={(e) => { e.currentTarget.style.transform = 'none'; e.currentTarget.style.boxShadow = '2px 2px 0px #0f172a'; }}
              >
                Close Space
              </button>
            </div>

            {/* Modal Content - Two Columns */}
            <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
              
              {/* Left Column: Markdown Notes (60% width) */}
              <div style={{
                width: '60%',
                padding: '2rem',
                overflowY: 'auto',
                fontSize: '0.9rem',
                lineHeight: '1.6',
                color: '#334155',
                backgroundColor: '#f8fafc',
                borderRight: '3px solid #0f172a'
              }} className="fullscreen-markdown-body">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{studyNotes}</ReactMarkdown>
              </div>

              {/* Right Column: Scoped Chat Section (40% width) */}
              <div style={{
                width: '40%',
                display: 'flex',
                flexDirection: 'column',
                backgroundColor: 'white',
                padding: '1.5rem',
                boxSizing: 'border-box'
              }}>
                <h4 style={{ fontSize: '0.9rem', fontWeight: 800, color: '#0f172a', display: 'flex', alignItems: 'center', gap: '0.35rem', marginTop: 0, marginBottom: '0.75rem' }}>
                  <HelpIcon size={16} style={{ color: 'var(--primary)' }} /> Clarify Doubts with AI Tutor
                </h4>

                {/* Message list */}
                <div className="chat-messages" style={{ flex: 1, height: 'auto', marginBottom: '1rem', border: '1.5px solid #0f172a', borderRadius: '0.5rem', padding: '1rem', background: '#fafafa' }}>
                  {chatMessages.length === 0 ? (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', gap: '0.5rem', textAlign: 'center' }}>
                      <HelpCircle size={24} style={{ opacity: 0.4 }} />
                      <p style={{ fontSize: '0.8rem' }}>Ask any questions about the concepts shown on the left!</p>
                    </div>
                  ) : (
                    chatMessages.map((m, i) => (
                      <div key={i} className={`message-bubble ${m.role}`}>
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.content}</ReactMarkdown>
                      </div>
                    ))
                  )}
                  {chatLoading && <div className="message-bubble assistant">Thinking...</div>}
                  <div ref={modalChatEndRef} />
                </div>

                {/* Message input */}
                <form onSubmit={handleSendMessage} className="chat-input-row" style={{ marginTop: 'auto' }}>
                  <input 
                    type="text" 
                    placeholder="Ask AI a question about this guide..." 
                    value={chatInput}
                    onChange={(e) => setChatInput(e.target.value)} 
                    disabled={chatLoading} 
                    style={{ padding: '0.6rem 0.8rem', fontSize: '0.85rem' }} 
                  />
                  <button 
                    type="submit" 
                    className="chat-send-btn" 
                    disabled={chatLoading} 
                    style={{ padding: '0 1rem' }}
                  >
                    <Send size={14} />
                  </button>
                </form>
              </div>

            </div>

            {/* Modal Footer */}
            <div style={{
              padding: '0.75rem 1.5rem',
              borderTop: '3px solid #0f172a',
              backgroundColor: 'white',
              display: 'flex',
              justifyContent: 'flex-end',
              gap: '1rem'
            }}>
              <button 
                onClick={() => setIsNotesFullscreen(false)}
                style={{
                  padding: '0.45rem 1rem',
                  backgroundColor: 'white',
                  border: '2px solid #0f172a',
                  borderRadius: '0.35rem',
                  fontWeight: 700,
                  cursor: 'pointer',
                  fontSize: '0.8rem',
                  boxShadow: '2px 2px 0px #0f172a'
                }}
              >
                Back to Map
              </button>
              <button 
                onClick={() => {
                  setIsNotesFullscreen(false);
                  setActiveTab('quiz');
                }}
                style={{
                  padding: '0.45rem 1.2rem',
                  backgroundColor: 'var(--primary)',
                  color: 'white',
                  border: '2px solid #0f172a',
                  borderRadius: '0.35rem',
                  fontWeight: 700,
                  cursor: 'pointer',
                  fontSize: '0.8rem',
                  boxShadow: '2px 2px 0px #0f172a',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.35rem'
                }}
              >
                <Award size={14} /> Take Quiz
              </button>
            </div>
          </div>
        </div>
      )}

    </div>
  );
};
