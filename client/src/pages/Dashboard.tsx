import React, { useState, useEffect, useRef } from 'react';
import { 
  BookOpen, ChevronRight, ChevronDown, Lock, 
  Send, RefreshCw, Network, MessageSquare, Award, 
  HelpCircle, CheckCircle2, AlertTriangle, Play, 
  Trash2, Pencil, Check, X, Sparkles
} from 'lucide-react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

interface TopicNode {
  id: string;
  name: string;
  level: number;
  description: string;
  children: TopicNode[];
}

interface Progress {
  topic_id: string;
  mastery_score: number;
  last_reviewed?: string;
  notes: string;
}

interface TopicDetail {
  id: string;
  name: string;
  level: number;
  description: string;
  artifact_type?: string;
  progress?: Progress;
  prerequisites: any[];
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

interface DecomposeProposal {
  id: string;
  name: string;
  description: string;
  artifact_type: string;
  prerequisites: string[];
  selected: boolean;
}

export const Dashboard: React.FC = () => {
  const [tree, setTree] = useState<TopicNode[]>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [nodeDetail, setNodeDetail] = useState<TopicDetail | null>(null);
  const [relations, setRelations] = useState<TopicEdge[]>([]);
  const [expandedNodes, setExpandedNodes] = useState<Record<string, boolean>>({});

  // Full graph states
  const [fullGraph, setFullGraph] = useState<{ nodes: TopicDetail[]; edges: TopicEdge[] }>({ nodes: [], edges: [] });
  const [nodePositions, setNodePositions] = useState<Record<string, { x: number; y: number }>>(() => {
    try {
      const stored = localStorage.getItem('converseai_node_positions');
      return stored ? JSON.parse(stored) : {};
    } catch {
      return {};
    }
  });
  const [draggingNodeId, setDraggingNodeId] = useState<string | null>(null);
  const svgRef = useRef<SVGSVGElement | null>(null);
  const dragStartRef = useRef<{ x: number; y: number } | null>(null);

  // Save positions when they change
  useEffect(() => {
    localStorage.setItem('converseai_node_positions', JSON.stringify(nodePositions));
  }, [nodePositions]);
  
  // Dashboard states
  const [dailyAgenda, setDailyAgenda] = useState<TopicDetail[]>([]);
  const [weeklyReviewReport, setWeeklyReviewReport] = useState<string>('');
  const [loadingReview, setLoadingReview] = useState(false);

  // Planning chat states
  const [planMessages, setPlanMessages] = useState<ChatMessage[]>([]);
  const [planInput, setPlanInput] = useState('');
  const [planLoading, setPlanLoading] = useState(false);
  const planEndRef = useRef<HTMLDivElement>(null);

  // Scoped chat states
  const [chatMessages, setChatMessages] = useState<ChatMessage[]>([]);
  const [chatInput, setChatInput] = useState('');
  const [chatLoading, setChatLoading] = useState(false);
  const chatEndRef = useRef<HTMLDivElement>(null);

  // Progress update states
  const [masteryInput, setMasteryInput] = useState<number>(0);
  const [notesInput, setNotesInput] = useState<string>('');
  const [savingProgress, setSavingProgress] = useState(false);

  // Decompose preview/confirm states
  const [decomposing, setDecomposing] = useState(false);
  const [decomposeProposals, setDecomposeProposals] = useState<DecomposeProposal[] | null>(null);
  const [confirmingDecompose, setConfirmingDecompose] = useState(false);

  // Inline edit states
  const [editingName, setEditingName] = useState(false);
  const [editingDesc, setEditingDesc] = useState(false);
  const [editNameValue, setEditNameValue] = useState('');
  const [editDescValue, setEditDescValue] = useState('');

  // Delete state
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  useEffect(() => {
    fetchTree();
    fetchDailyAgenda();
    fetchFullGraph();
  }, []);

  useEffect(() => {
    if (selectedNodeId) {
      fetchNodeDetail(selectedNodeId);
      fetchRelations(selectedNodeId);
      setChatMessages([]);
      setDecomposeProposals(null);
      setShowDeleteConfirm(false);
      setEditingName(false);
      setEditingDesc(false);
    } else {
      setNodeDetail(null);
      setRelations([]);
      fetchDailyAgenda();
      fetchFullGraph();
    }
  }, [selectedNodeId]);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [chatMessages]);

  useEffect(() => {
    planEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [planMessages]);

  const fetchTree = async () => {
    try {
      const res = await fetch('/api/topics/tree');
      if (res.ok) {
        const data = await res.json();
        const safeData = data || [];
        setTree(safeData);
        const expansions: Record<string, boolean> = {};
        safeData.forEach((node: TopicNode) => {
          expansions[node.id] = true;
        });
        setExpandedNodes(expansions);
      }
    } catch (err) {
      console.error('Error fetching tree:', err);
    }
  };

  const fetchDailyAgenda = async () => {
    try {
      const res = await fetch('/api/agenda/today');
      if (res.ok) {
        const data = await res.json();
        setDailyAgenda(data || []);
      }
    } catch (err) {
      console.error('Error fetching agenda:', err);
    }
  };

  const fetchFullGraph = async () => {
    try {
      const res = await fetch('/api/topics/all_graph');
      if (res.ok) {
        const data = await res.json();
        setFullGraph(data || { nodes: [], edges: [] });
      }
    } catch (err) {
      console.error('Error fetching full graph:', err);
    }
  };

  // Position calculation effect
  useEffect(() => {
    if (!fullGraph.nodes || fullGraph.nodes.length === 0) return;

    let changed = false;
    const updated = { ...nodePositions };

    // Sort nodes by level so parents are processed before children
    const sortedNodes = [...fullGraph.nodes].sort((a, b) => (a.level || 1) - (b.level || 1));

    sortedNodes.forEach((node) => {
      if (!updated[node.id]) {
        changed = true;
        // Find if this node has a parent
        const parentEdge = fullGraph.edges.find(e => e.edge_type === 'part_of' && e.from_id === node.id);
        if (parentEdge && updated[parentEdge.to_id]) {
          const parentPos = updated[parentEdge.to_id];
          // Place below parent with a small random jitter to avoid exact overlap
          const jitter = (Math.random() - 0.5) * 60;
          updated[node.id] = {
            x: parentPos.x + jitter,
            y: parentPos.y + 120
          };
        } else {
          // If no parent pos is resolved yet, layout by level rows
          const level = node.level || 1;
          const levelNodes = fullGraph.nodes.filter(n => (n.level || 1) === level);
          const index = levelNodes.findIndex(n => n.id === node.id);
          const siblingCount = levelNodes.length;

          const width = 800;
          const x = siblingCount > 1 
            ? 100 + (index / (siblingCount - 1)) * (width - 200) 
            : width / 2;
          const y = 60 + (level - 1) * 120;

          updated[node.id] = { x, y };
        }
      }
    });

    if (changed) {
      setNodePositions(updated);
    }
  }, [fullGraph]);

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!draggingNodeId || !svgRef.current) return;
    const rect = svgRef.current.getBoundingClientRect();
    
    // Adjust mouse coordinate to SVG coordinate system (viewBox is 0 0 800 500)
    const x = ((e.clientX - rect.left) / rect.width) * 800;
    const y = ((e.clientY - rect.top) / rect.height) * 500;
    
    setNodePositions(prev => ({
      ...prev,
      [draggingNodeId]: {
        x: Math.max(25, Math.min(775, x)),
        y: Math.max(25, Math.min(475, y))
      }
    }));
  };

  const handleMouseUp = () => {
    setDraggingNodeId(null);
  };

  const handleNodeMouseDown = (nodeId: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setDraggingNodeId(nodeId);
    dragStartRef.current = { x: e.clientX, y: e.clientY };
  };

  const handleNodeMouseUp = (nodeId: string, e: React.MouseEvent) => {
    if (dragStartRef.current) {
      const dx = e.clientX - dragStartRef.current.x;
      const dy = e.clientY - dragStartRef.current.y;
      const dist = Math.sqrt(dx * dx + dy * dy);
      if (dist < 5) {
        // It was a click, not a drag! Select the node.
        setSelectedNodeId(nodeId);
      }
    }
    dragStartRef.current = null;
    setDraggingNodeId(null);
  };

  const fetchNodeDetail = async (id: string) => {
    try {
      const res = await fetch(`/api/topics/get?id=${id}`);
      if (res.ok) {
        const data = await res.json();
        setNodeDetail(data);
        setMasteryInput(data.progress?.mastery_score || 0);
        setNotesInput(data.progress?.notes || '');
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

  // --- Planning Chat ---
  const handlePlanSend = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!planInput.trim() || planLoading) return;

    const userMsg: ChatMessage = { role: 'user', content: planInput.trim() };
    const newMessages = [...planMessages, userMsg];
    setPlanMessages(newMessages);
    setPlanInput('');
    setPlanLoading(true);

    try {
      const res = await fetch('/api/topics/plan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ messages: newMessages }),
      });
      if (res.ok) {
        const data = await res.json();
        setPlanMessages([...newMessages, { role: 'assistant', content: data.response }]);
        if (data.graph_updated) {
          fetchTree();
          fetchDailyAgenda();
          fetchFullGraph();
        }
      }
    } catch (err) {
      console.error('Planning chat error:', err);
    } finally {
      setPlanLoading(false);
    }
  };

  // --- Decompose Preview/Confirm ---
  const handleDecomposePreview = async () => {
    if (!selectedNodeId) return;
    setDecomposing(true);
    setDecomposeProposals(null);
    try {
      const res = await fetch(`/api/topics/decompose/preview?id=${selectedNodeId}`, { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        const proposals = (data.sub_topics || []).map((st: any) => ({ ...st, selected: true }));
        setDecomposeProposals(proposals);
      }
    } catch (err) {
      console.error('Decompose preview error:', err);
    } finally {
      setDecomposing(false);
    }
  };

  const toggleProposal = (idx: number) => {
    if (!decomposeProposals) return;
    const updated = [...decomposeProposals];
    updated[idx] = { ...updated[idx], selected: !updated[idx].selected };
    setDecomposeProposals(updated);
  };

  const handleDecomposeConfirm = async () => {
    if (!selectedNodeId || !decomposeProposals) return;
    const selected = decomposeProposals.filter(p => p.selected);
    if (selected.length === 0) {
      setDecomposeProposals(null);
      return;
    }
    setConfirmingDecompose(true);
    try {
      const res = await fetch('/api/topics/decompose/confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          parent_id: selectedNodeId,
          sub_topics: selected.map(({ selected: _, ...rest }) => rest),
        }),
      });
      if (res.ok) {
        setDecomposeProposals(null);
        fetchTree();
        fetchNodeDetail(selectedNodeId);
        fetchRelations(selectedNodeId);
        fetchFullGraph();
      }
    } catch (err) {
      console.error('Decompose confirm error:', err);
    } finally {
      setConfirmingDecompose(false);
    }
  };

  // --- Scoped Chat ---
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

  // --- Progress ---
  const handleSaveProgress = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedNodeId) return;
    setSavingProgress(true);
    try {
      await fetch('/api/topics/progress', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ topic_id: selectedNodeId, mastery_score: masteryInput, notes: notesInput }),
      });
      fetchNodeDetail(selectedNodeId);
      fetchDailyAgenda();
      fetchFullGraph();
    } catch (err) {
      console.error('Save progress error:', err);
    } finally {
      setSavingProgress(false);
    }
  };

  // --- Inline Edit ---
  const startEditName = () => {
    if (!nodeDetail) return;
    setEditNameValue(nodeDetail.name);
    setEditingName(true);
  };

  const saveEditName = async () => {
    if (!nodeDetail) return;
    try {
      await fetch('/api/topics/edit', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: nodeDetail.id, name: editNameValue, description: nodeDetail.description }),
      });
      fetchNodeDetail(nodeDetail.id);
      fetchTree();
      fetchFullGraph();
    } finally {
      setEditingName(false);
    }
  };

  const startEditDesc = () => {
    if (!nodeDetail) return;
    setEditDescValue(nodeDetail.description);
    setEditingDesc(true);
  };

  const saveEditDesc = async () => {
    if (!nodeDetail) return;
    try {
      await fetch('/api/topics/edit', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: nodeDetail.id, name: nodeDetail.name, description: editDescValue }),
      });
      fetchNodeDetail(nodeDetail.id);
      fetchFullGraph();
    } finally {
      setEditingDesc(false);
    }
  };

  // --- Delete ---
  const handleDeleteTopic = async () => {
    if (!selectedNodeId) return;
    try {
      const res = await fetch(`/api/topics/delete?id=${selectedNodeId}`, { method: 'DELETE' });
      if (res.ok) {
        setSelectedNodeId(null);
        setShowDeleteConfirm(false);
        fetchTree();
        fetchFullGraph();
      }
    } catch (err) {
      console.error('Delete error:', err);
    }
  };

  // --- Weekly Review ---
  const runWeeklyReview = async () => {
    setLoadingReview(true);
    try {
      const res = await fetch('/api/review/weekly', { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        setWeeklyReviewReport(data.report);
      }
    } catch (err) {
      console.error('Weekly review error:', err);
    } finally {
      setLoadingReview(false);
    }
  };

  const toggleExpand = (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setExpandedNodes(prev => ({ ...prev, [id]: !prev[id] }));
  };

  // --- SVG Graph ---
  const renderTotalGraph = () => {
    if (!fullGraph.nodes || fullGraph.nodes.length === 0) {
      return (
        <div style={{ padding: '3rem 1rem', textAlign: 'center', color: 'var(--text-muted)' }}>
          <Sparkles size={32} style={{ margin: '0 auto 0.75rem', opacity: 0.5 }} />
          <p>No topics to display in the graph yet.</p>
        </div>
      );
    }

    const lines: React.ReactNode[] = [];
    fullGraph.edges.forEach((edge, idx) => {
      const sourcePos = nodePositions[edge.from_id];
      const targetPos = nodePositions[edge.to_id];
      if (sourcePos && targetPos) {
        const dx = targetPos.x - sourcePos.x;
        const dy = targetPos.y - sourcePos.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        
        if (dist > 44) {
          const r = 22;
          const x1 = sourcePos.x + (dx / dist) * r;
          const y1 = sourcePos.y + (dy / dist) * r;
          const x2 = targetPos.x - (dx / dist) * r;
          const y2 = targetPos.y - (dy / dist) * r;

          const isPartOf = edge.edge_type === 'part_of';
          lines.push(
            <line
              key={`edge-${idx}`}
              x1={x1}
              y1={y1}
              x2={x2}
              y2={y2}
              stroke={isPartOf ? '#cbd5e1' : '#64748b'}
              strokeWidth={2}
              strokeDasharray={isPartOf ? '5 5' : undefined}
              markerEnd={isPartOf ? undefined : 'url(#arrow-main)'}
            />
          );
        }
      }
    });

    return (
      <svg
        ref={svgRef}
        className="svg-graph-container"
        viewBox="0 0 800 500"
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseUp}
        style={{
          width: '100%',
          height: '500px',
          border: '1px solid var(--border)',
          borderRadius: '1rem',
          backgroundColor: '#f8fafc',
          userSelect: 'none',
        }}
      >
        <defs>
          <marker
            id="arrow-main"
            viewBox="0 0 10 10"
            refX="6"
            refY="5"
            markerWidth="6"
            markerHeight="6"
            orient="auto-start-reverse"
          >
            <path d="M 0 1.5 L 8 5 L 0 8.5 z" fill="#64748b" />
          </marker>
        </defs>

        {/* Lines */}
        {lines}

        {/* Nodes */}
        {fullGraph.nodes.map((node) => {
          const pos = nodePositions[node.id];
          if (!pos) return null;

          const mastery = node.progress?.mastery_score || 0;
          const isLocked = node.locked;
          
          let circleFill = '#f1f5f9';
          let circleStroke = '#94a3b8';
          let textColor = '#475569';

          if (isLocked) {
            circleFill = '#f8fafc';
            circleStroke = '#cbd5e1';
          } else if (mastery >= 70) {
            circleFill = '#d1fae5';
            circleStroke = '#10b981';
            textColor = '#065f46';
          } else if (mastery > 0) {
            circleFill = '#fef3c7';
            circleStroke = '#f59e0b';
            textColor = '#92400e';
          }

          return (
            <g
              key={node.id}
              transform={`translate(${pos.x}, ${pos.y})`}
              onMouseDown={(e) => handleNodeMouseDown(node.id, e)}
              onMouseUp={(e) => handleNodeMouseUp(node.id, e)}
              style={{ cursor: draggingNodeId === node.id ? 'grabbing' : 'grab' }}
            >
              {/* Background Glow if active */}
              {selectedNodeId === node.id && (
                <circle r={28} fill="none" stroke="var(--primary)" strokeWidth={2} strokeDasharray="3 3" />
              )}
              
              {/* Main Node Circle */}
              <circle
                r={22}
                fill={circleFill}
                stroke={circleStroke}
                strokeWidth={2.5}
                style={{
                  transition: draggingNodeId === node.id ? 'none' : 'transform 0.1s ease',
                  filter: 'drop-shadow(0 2px 4px rgba(0,0,0,0.05))',
                }}
              />

              {/* Locked Padlock or Mastery Score */}
              {isLocked ? (
                <g transform="translate(0, 0)">
                  <rect x="-5" y="-1" width="10" height="7" rx="1" fill="#cbd5e1" />
                  <path d="M -3.5 -1 V -3 A 3.5 3.5 0 0 1 3.5 -1 V -1" fill="none" stroke="#cbd5e1" strokeWidth="1.2" />
                </g>
              ) : (
                <text
                  textAnchor="middle"
                  dy="3.5px"
                  fontSize="10px"
                  fontWeight="700"
                  fill={textColor}
                >
                  {mastery}%
                </text>
              )}

              {/* Node Label Card/Pill Background */}
              <rect
                x={-Math.min(node.name.length * 3.5 + 8, 80)}
                y={28}
                width={Math.min(node.name.length * 7 + 16, 160)}
                height={18}
                rx={4}
                fill="white"
                stroke={selectedNodeId === node.id ? 'var(--primary)' : 'var(--border)'}
                strokeWidth={selectedNodeId === node.id ? 1.5 : 1}
                style={{ filter: 'drop-shadow(0 1px 2px rgba(0,0,0,0.05))' }}
              />

              {/* Node Label Text */}
              <text
                y={40}
                textAnchor="middle"
                fontSize="10px"
                fontWeight="600"
                fill="#1e293b"
              >
                {node.name.length > 20 ? node.name.slice(0, 18) + '...' : node.name}
              </text>
            </g>
          );
        })}
      </svg>
    );
  };

  const renderLocalGraph = () => {
    if (!selectedNodeId || !nodeDetail) return null;

    const centerX = 200;
    const centerY = 175;
    
    const parents = relations.filter(r => r.edge_type === 'part_of' && r.from_id === selectedNodeId);
    const children = relations.filter(r => r.edge_type === 'part_of' && r.to_id === selectedNodeId);
    const prereqs = relations.filter(r => r.edge_type === 'prerequisite_of' && r.to_id === selectedNodeId);
    const dependencies = relations.filter(r => r.edge_type === 'prerequisite_of' && r.from_id === selectedNodeId);

    const nodes: { id: string; label: string; x: number; y: number; role: string }[] = [
      { id: selectedNodeId, label: nodeDetail.name, x: centerX, y: centerY, role: 'center' }
    ];
    const lines: { x1: number; y1: number; x2: number; y2: number; type: string }[] = [];

    if (parents.length > 0) {
      nodes.push({ id: parents[0].to_id, label: 'Parent', x: centerX, y: 50, role: 'parent' });
      lines.push({ x1: centerX, y1: centerY, x2: centerX, y2: 50, type: 'part_of' });
    }

    children.forEach((child, i) => {
      const x = 100 + i * 100;
      nodes.push({ id: child.from_id, label: child.from_id.replace('t_', ''), x, y: 300, role: 'child' });
      lines.push({ x1: centerX, y1: centerY, x2: x, y2: 300, type: 'part_of' });
    });

    prereqs.forEach((prereq, i) => {
      nodes.push({ id: prereq.from_id, label: prereq.from_id.replace('t_', ''), x: 50, y: 100 + i * 75, role: 'prereq' });
      lines.push({ x1: 50, y1: 100 + i * 75, x2: centerX, y2: centerY, type: 'prereq' });
    });

    dependencies.forEach((dep, i) => {
      nodes.push({ id: dep.to_id, label: dep.to_id.replace('t_', ''), x: 350, y: 100 + i * 75, role: 'dependency' });
      lines.push({ x1: centerX, y1: centerY, x2: 350, y2: 100 + i * 75, type: 'prereq' });
    });

    return (
      <svg className="svg-graph-container" viewBox="0 0 400 350">
        <defs>
          <marker id="arrow" viewBox="0 0 10 10" refX="15" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z" fill="#64748b" />
          </marker>
        </defs>
        {lines.map((l, i) => (
          <line key={i} x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2}
            stroke={l.type === 'part_of' ? '#cbd5e1' : '#64748b'} strokeWidth={2}
            strokeDasharray={l.type === 'part_of' ? '4 4' : 'none'}
            markerEnd={l.type === 'prereq' ? 'url(#arrow)' : undefined} />
        ))}
        {nodes.map(n => {
          const isCenter = n.role === 'center';
          return (
            <g key={n.id} transform={`translate(${n.x}, ${n.y})`}
              onClick={() => n.id !== selectedNodeId && setSelectedNodeId(n.id)}
              style={{ cursor: n.id !== selectedNodeId ? 'pointer' : 'default' }}>
              <circle r={isCenter ? 24 : 18} fill={isCenter ? '#6366f1' : '#ffffff'}
                stroke={isCenter ? '#4f46e5' : '#cbd5e1'} strokeWidth={2} />
              <text y={isCenter ? 38 : 30} textAnchor="middle"
                fontSize={isCenter ? '10px' : '9px'} fontWeight={isCenter ? 'bold' : 'normal'} fill="#1e293b">
                {n.label}
              </text>
            </g>
          );
        })}
      </svg>
    );
  };

  const renderSidebarNode = (node: TopicNode) => {
    const isExpanded = !!expandedNodes[node.id];
    const hasChildren = node.children && node.children.length > 0;
    const isActive = selectedNodeId === node.id;

    return (
      <div key={node.id} className="tree-node-wrapper">
        <div className={`tree-node-item ${isActive ? 'active' : ''}`} onClick={() => setSelectedNodeId(node.id)}>
          {hasChildren ? (
            <span onClick={(e) => toggleExpand(node.id, e)}>
              {isExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
            </span>
          ) : (
            <BookOpen size={16} />
          )}
          <span style={{ flex: 1, textOverflow: 'ellipsis', overflow: 'hidden', whiteSpace: 'nowrap' }}>
            {node.name}
          </span>
        </div>
        {hasChildren && isExpanded && (
          <div className="tree-node">
            {node.children.map(child => renderSidebarNode(child))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="explorer-layout">
      {/* LEFT COLUMN: Sidebar Navigation */}
      <aside className="explorer-sidebar">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
          <h2 style={{ fontSize: '1.1rem', fontWeight: 700, color: '#0f172a' }}>Topic Explorer</h2>
          {selectedNodeId && (
            <button onClick={() => setSelectedNodeId(null)}
              style={{ background: 'none', border: 'none', color: '#6366f1', fontSize: '0.8rem', cursor: 'pointer', fontWeight: 600 }}>
              Dashboard
            </button>
          )}
        </div>
        <div className="tree-container">
          {tree.length === 0 ? (
            <div style={{ padding: '2rem 1rem', textAlign: 'center', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
              <Sparkles size={24} style={{ margin: '0 auto 0.75rem', opacity: 0.5 }} />
              <p>No topics yet. Use the planning chat to tell us what you want to learn!</p>
            </div>
          ) : (
            tree.map(node => renderSidebarNode(node))
          )}
        </div>
      </aside>

      {/* RIGHT COLUMN */}
      <main className="explorer-content">
        {nodeDetail ? (
          <div>
            {/* Header with inline edit + delete */}
            <div className="node-header">
              <div className="node-title-row">
                <div style={{ flex: 1 }}>
                  {editingName ? (
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <input type="text" value={editNameValue} onChange={e => setEditNameValue(e.target.value)}
                        onKeyDown={e => e.key === 'Enter' && saveEditName()}
                        style={{ fontSize: '1.5rem', fontWeight: 700, border: '1px solid var(--primary)', borderRadius: '0.5rem', padding: '0.25rem 0.5rem', width: '100%' }}
                        autoFocus />
                      <button onClick={saveEditName} className="icon-btn success"><Check size={18} /></button>
                      <button onClick={() => setEditingName(false)} className="icon-btn"><X size={18} /></button>
                    </div>
                  ) : (
                    <h1 style={{ fontSize: '1.75rem', fontWeight: 800, color: '#0f172a', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '0.5rem' }}
                      onClick={startEditName}>
                      {nodeDetail.name}
                      <Pencil size={14} style={{ color: 'var(--text-muted)', opacity: 0.4 }} />
                    </h1>
                  )}
                  <p style={{ color: 'var(--text-muted)', fontSize: '0.9rem', marginTop: '0.25rem' }}>Level: {nodeDetail.level}</p>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                  {nodeDetail.progress && (
                    <span className="mastery-badge">Mastery: {nodeDetail.progress.mastery_score}%</span>
                  )}
                  {showDeleteConfirm ? (
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <span style={{ fontSize: '0.8rem', color: 'var(--error)' }}>Delete?</span>
                      <button onClick={handleDeleteTopic} className="icon-btn danger"><Check size={16} /></button>
                      <button onClick={() => setShowDeleteConfirm(false)} className="icon-btn"><X size={16} /></button>
                    </div>
                  ) : (
                    <button onClick={() => setShowDeleteConfirm(true)} className="icon-btn danger-hover" title="Delete topic">
                      <Trash2 size={16} />
                    </button>
                  )}
                </div>
              </div>
              
              {nodeDetail.progress && (
                <div className="progress-bar-container">
                  <div className="progress-bar" style={{ width: `${nodeDetail.progress.mastery_score}%` }}></div>
                </div>
              )}
            </div>

            {/* Locked Warning */}
            {nodeDetail.locked && (
              <div className="locked-alert">
                <Lock size={18} style={{ flexShrink: 0, marginTop: '2px' }} />
                <div>
                  <h4 style={{ fontWeight: 600 }}>Topic Prerequisites Locked</h4>
                  <p style={{ fontSize: '0.85rem', marginTop: '0.25rem' }}>
                    Master (&ge;70%) the following prerequisites to unlock:
                  </p>
                  <ul style={{ paddingLeft: '1.25rem', marginTop: '0.5rem', fontSize: '0.85rem' }}>
                    {nodeDetail.prerequisites.map((p: any) => (
                      <li key={p.id} style={{ cursor: 'pointer', textDecoration: 'underline' }} onClick={() => setSelectedNodeId(p.id)}>
                        {p.name}
                      </li>
                    ))}
                  </ul>
                </div>
              </div>
            )}

            {/* Description (inline editable) */}
            {editingDesc ? (
              <div style={{ marginBottom: '2rem' }}>
                <textarea value={editDescValue} onChange={e => setEditDescValue(e.target.value)}
                  rows={3}
                  style={{ width: '100%', padding: '0.75rem', borderRadius: '0.75rem', border: '1px solid var(--primary)', fontSize: '1rem', fontFamily: 'inherit', resize: 'vertical' }}
                  autoFocus />
                <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.5rem' }}>
                  <button onClick={saveEditDesc} className="icon-btn success"><Check size={16} /> Save</button>
                  <button onClick={() => setEditingDesc(false)} className="icon-btn"><X size={16} /> Cancel</button>
                </div>
              </div>
            ) : (
              <p style={{ fontSize: '1.05rem', color: '#334155', marginBottom: '2rem', lineHeight: '1.6', cursor: 'pointer' }}
                onClick={startEditDesc}>
                {nodeDetail.description}
                <Pencil size={12} style={{ marginLeft: '0.5rem', color: 'var(--text-muted)', opacity: 0.4 }} />
              </p>
            )}

            {/* Action Grid */}
            <div className="node-grid">
              
              {/* Box 1: SVG Graph & Decomposition */}
              <div className="detail-card">
                <h3><Network size={18} /> Relations Graph</h3>
                {renderLocalGraph()}
                
                {/* Decompose Preview/Confirm */}
                {!nodeDetail.artifact_type && (
                  <div style={{ marginTop: '1.25rem' }}>
                    {decomposeProposals ? (
                      <div>
                        <h4 style={{ fontSize: '0.9rem', fontWeight: 600, marginBottom: '0.75rem', color: '#0f172a' }}>
                          Proposed Sub-Topics (select to approve):
                        </h4>
                        {decomposeProposals.map((p, i) => (
                          <label key={p.id} className={`proposal-item ${p.selected ? 'selected' : ''}`}>
                            <input type="checkbox" checked={p.selected} onChange={() => toggleProposal(i)} />
                            <div>
                              <strong>{p.name}</strong>
                              <p style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginTop: '0.15rem' }}>{p.description}</p>
                            </div>
                          </label>
                        ))}
                        <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1rem' }}>
                          <button onClick={handleDecomposeConfirm} className="auth-button" style={{ padding: '0.5rem', flex: 1 }}
                            disabled={confirmingDecompose}>
                            {confirmingDecompose ? <RefreshCw className="animate-spin" size={16} /> : <Check size={16} />}
                            Confirm Selected
                          </button>
                          <button onClick={() => setDecomposeProposals(null)} className="auth-button"
                            style={{ padding: '0.5rem', flex: 1, background: 'var(--bg-subtle)', color: 'var(--text)', border: '1px solid var(--border)' }}>
                            <X size={16} /> Cancel
                          </button>
                        </div>
                      </div>
                    ) : (
                      <button onClick={handleDecomposePreview} disabled={decomposing} className="auth-button" style={{ padding: '0.6rem' }}>
                        {decomposing ? <RefreshCw className="animate-spin" size={16} /> : <Play size={16} />}
                        {decomposing ? 'Generating proposals...' : 'Decompose Topic Node'}
                      </button>
                    )}
                  </div>
                )}
              </div>

              {/* Box 2: Scoped Tutor Chat */}
              <div className="detail-card" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
                <div>
                  <h3><MessageSquare size={18} /> Scoped AI Tutor</h3>
                  <div className="chat-messages">
                    {chatMessages.length === 0 ? (
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', gap: '0.5rem' }}>
                        <HelpCircle size={32} />
                        <p style={{ fontSize: '0.85rem' }}>Ask your personal tutor about {nodeDetail.name}!</p>
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
                </div>
                <form onSubmit={handleSendMessage} className="chat-input-row">
                  <input type="text" placeholder="Ask a question..." value={chatInput}
                    onChange={(e) => setChatInput(e.target.value)} disabled={chatLoading} />
                  <button type="submit" className="chat-send-btn" disabled={chatLoading}><Send size={16} /></button>
                </form>
              </div>

              {/* Box 3: Update Progress (full span) */}
              <div className="detail-card" style={{ gridColumn: 'span 2' }}>
                <h3><Award size={18} /> Update Mastery Progress</h3>
                <form onSubmit={handleSaveProgress} style={{ display: 'grid', gridTemplateColumns: '150px 1fr auto', gap: '1.5rem', alignItems: 'end' }}>
                  <div className="form-group" style={{ marginBottom: 0 }}>
                    <label>Mastery Score (0-100)</label>
                    <input type="number" min={0} max={100} value={masteryInput}
                      onChange={(e) => setMasteryInput(Number(e.target.value))} style={{ paddingLeft: '1rem' }} />
                  </div>
                  <div className="form-group" style={{ marginBottom: 0 }}>
                    <label>Study Notes & Obstacles</label>
                    <input type="text" placeholder="What concepts are hard? What went well?"
                      value={notesInput} onChange={(e) => setNotesInput(e.target.value)} style={{ paddingLeft: '1rem' }} />
                  </div>
                  <button type="submit" className="auth-button" style={{ width: 'auto', padding: '0.75rem 2rem' }} disabled={savingProgress}>
                    {savingProgress ? 'Saving...' : 'Save Progress'}
                  </button>
                </form>
              </div>
            </div>
          </div>
        ) : (
          /* HIGH LEVEL DASHBOARD VIEW */
          <div>
            <div className="node-header" style={{ marginBottom: '2rem' }}>
              <h1 style={{ fontSize: '2rem', fontWeight: 800, color: '#0f172a' }}>Knowledge Explorer</h1>
              <p style={{ color: 'var(--text-muted)' }}>Tell me what you want to learn, and I'll build your study plan.</p>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '2rem' }}>
              
              {/* Planning Chat */}
              <div className="detail-card planning-chat-card">
                <h3><Sparkles size={18} /> Planning Chat</h3>
                <div className="chat-messages planning-messages">
                  {planMessages.length === 0 ? (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', gap: '0.75rem', padding: '2rem' }}>
                      <Sparkles size={36} style={{ opacity: 0.3 }} />
                      <p style={{ fontSize: '0.9rem', textAlign: 'center', lineHeight: '1.6' }}>
                        Tell me what you want to learn!<br />
                        <em>"I want to prepare for Google interviews"</em><br />
                        <em>"Help me learn Kubernetes"</em><br />
                        <em>"Add machine learning to my plan"</em>
                      </p>
                    </div>
                  ) : (
                    planMessages.map((m, i) => (
                      <div key={i} className={`message-bubble ${m.role}`}>
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{m.content}</ReactMarkdown>
                      </div>
                    ))
                  )}
                  {planLoading && <div className="message-bubble assistant"><RefreshCw className="animate-spin" size={16} /> Building your plan...</div>}
                  <div ref={planEndRef} />
                </div>
                <form onSubmit={handlePlanSend} className="chat-input-row">
                  <input type="text" placeholder="What do you want to learn?" value={planInput}
                    onChange={(e) => setPlanInput(e.target.value)} disabled={planLoading} />
                  <button type="submit" className="chat-send-btn" disabled={planLoading}><Send size={16} /></button>
                </form>
              </div>

              {/* Right column: Agenda + Weekly Review */}
              <div style={{ display: 'flex', flexDirection: 'column', gap: '2rem' }}>
                
                {/* Daily Agenda */}
                <div>
                  <h2 style={{ fontSize: '1.25rem', fontWeight: 700, display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#0f172a' }}>
                    <CheckCircle2 size={20} style={{ color: 'var(--success)' }} /> Daily Study Agenda
                  </h2>
                  <p style={{ color: 'var(--text-muted)', fontSize: '0.9rem', marginBottom: '1rem' }}>
                    Top recommendations by Intelligent Prioritization Score.
                  </p>

                  {dailyAgenda.length === 0 ? (
                    <div style={{ border: '1px dashed var(--border)', borderRadius: '1rem', padding: '2rem', textAlign: 'center', color: 'var(--text-muted)' }}>
                      <BookOpen size={28} style={{ margin: '0 auto 0.75rem' }} />
                      <p style={{ fontSize: '0.85rem' }}>No recommendations yet. Start by telling the planner what you want to learn!</p>
                    </div>
                  ) : (
                    <div className="agenda-grid">
                      {dailyAgenda.map(item => (
                        <div key={item.id} className="agenda-card" onClick={() => setSelectedNodeId(item.id)}>
                          <div>
                            <span style={{ fontSize: '0.7rem', fontWeight: 600, color: 'var(--primary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                              {item.artifact_type?.replace('_', ' ')}
                            </span>
                            <h4 style={{ fontWeight: 600, marginTop: '0.2rem', fontSize: '1rem', color: '#0f172a' }}>{item.name}</h4>
                          </div>
                          <div style={{ marginTop: '0.75rem', borderTop: '1px solid var(--border)', paddingTop: '0.5rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                            <span style={{ fontSize: '0.8rem', fontWeight: 500, color: 'var(--text-muted)' }}>
                              {item.progress?.mastery_score || 0}%
                            </span>
                            <ChevronRight size={16} style={{ color: 'var(--primary)' }} />
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                {/* Weekly Review */}
                <div className="detail-card" style={{ height: 'fit-content' }}>
                  <h3 style={{ margin: 0 }}><AlertTriangle size={18} style={{ color: 'var(--warning)' }} /> Weekly Graph Review</h3>
                  <p style={{ fontSize: '0.85rem', color: 'var(--text-muted)', marginTop: '0.5rem', marginBottom: '1.5rem' }}>
                    Analyze mastery thresholds and bottleneck prerequisites.
                  </p>
                  <button onClick={runWeeklyReview} className="auth-button" style={{ padding: '0.6rem' }} disabled={loadingReview}>
                    {loadingReview ? 'Analyzing...' : 'Generate Review Report'}
                  </button>
                  {weeklyReviewReport && (
                    <div style={{ marginTop: '1.5rem', borderTop: '1px solid var(--border)', paddingTop: '1rem', fontSize: '0.9rem', lineHeight: '1.6', overflowY: 'auto', maxHeight: '300px' }}>
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>{weeklyReviewReport}</ReactMarkdown>
                    </div>
                  )}
                </div>

              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
};
