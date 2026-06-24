import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Target, Calendar, Trash2, ArrowRight } from 'lucide-react';

interface Task {
  id: string;
  title: string;
  status: string;
}

interface Project {
  id: string;
  title: string;
  target_date: string;
  status: string;
  tasks: Task[];
}

export const ProjectsList: React.FC = () => {
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  
  // Form State
  const [title, setTitle] = useState('');
  const [targetDate, setTargetDate] = useState('');
  const [creating, setCreating] = useState(false);
  const navigate = useNavigate();

  const fetchProjects = async () => {
    try {
      const response = await fetch('/api/projects');
      if (!response.ok) throw new Error('Failed to load projects');
      const data = await response.json();
      setProjects(data);
    } catch (err: any) {
      setError(err.message || 'An error occurred');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchProjects();
  }, []);

  const handleCreateProject = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title) return;

    setCreating(true);
    setError('');

    try {
      const response = await fetch('/api/projects', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title,
          target_date: targetDate || undefined
        }),
      });

      if (!response.ok) {
        throw new Error('Failed to decompose goal or create project');
      }

      const newProj = await response.json();
      // Redirect to the newly created project workspace
      navigate(`/projects/${newProj.id}`);
    } catch (err: any) {
      setError(err.message || 'Goal creation failed');
      setCreating(false);
    }
  };

  const handleDeleteProject = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation(); // Avoid triggering navigation to project
    if (!window.confirm('Are you sure you want to delete this project workspace?')) return;

    try {
      const response = await fetch(`/api/projects?id=${id}`, {
        method: 'DELETE',
      });
      if (!response.ok) throw new Error('Failed to delete project');
      setProjects(prev => prev.filter(p => p.id !== id));
    } catch (err: any) {
      alert(err.message);
    }
  };

  return (
    <div style={{ maxWidth: '900px', margin: '3rem auto', padding: '0 1.5rem 4rem' }}>
      <div style={{ marginBottom: '2.5rem' }}>
        <h2 style={{ fontSize: '2rem', fontWeight: 800, letterSpacing: '-0.025em', color: '#0f172a', marginBottom: '0.5rem' }}>
          Personal Execution Workspace
        </h2>
        <p style={{ color: '#64748b' }}>
          Set ambitious outcomes, decompose goals into structured milestones, and track execution competency.
        </p>
      </div>

      {error && (
        <div style={{ background: '#fef2f2', border: '1px solid #fee2e2', color: '#dc2626', padding: '1rem', borderRadius: '0.75rem', marginBottom: '2rem', fontSize: '0.9rem' }}>
          {error}
        </div>
      )}

      {/* Create Goal Form */}
      <div style={{ background: 'white', border: '1px solid #e2e8f0', borderRadius: '1.25rem', padding: '2rem', boxShadow: '0 4px 6px -1px rgba(0, 0, 0, 0.05)', marginBottom: '3rem' }}>
        <h3 style={{ fontSize: '1.15rem', fontWeight: 700, color: '#1e293b', marginBottom: '1.25rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <Target size={20} style={{ color: '#6366f1' }} /> Define a New North Star Goal
        </h3>
        
        <form onSubmit={handleCreateProject} style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 240px', gap: '1.25rem' }}>
            <div>
              <label style={{ display: 'block', fontSize: '0.85rem', fontWeight: 600, color: '#475569', marginBottom: '0.5rem' }}>North Star Goal</label>
              <input
                type="text"
                placeholder="e.g. Become Senior Engineer at Google, Clear MBA Admission, etc."
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                style={{ padding: '0.75rem 1rem', background: '#f8fafc', border: '1px solid #e2e8f0', borderRadius: '0.75rem', width: '100%', outline: 'none' }}
                disabled={creating}
              />
            </div>
            <div>
              <label style={{ display: 'block', fontSize: '0.85rem', fontWeight: 600, color: '#475569', marginBottom: '0.5rem' }}>Target Date (Optional)</label>
              <input
                type="date"
                value={targetDate}
                onChange={(e) => setTargetDate(e.target.value)}
                style={{ padding: '0.75rem 1rem', background: '#f8fafc', border: '1px solid #e2e8f0', borderRadius: '0.75rem', width: '100%', outline: 'none' }}
                disabled={creating}
              />
            </div>
          </div>

          <button
            type="submit"
            disabled={creating || !title}
            style={{
              alignSelf: 'flex-start',
              background: '#6366f1',
              color: 'white',
              border: 'none',
              padding: '0.8rem 1.5rem',
              borderRadius: '0.75rem',
              fontWeight: 600,
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              gap: '0.5rem',
              transition: 'all 0.2s',
              opacity: (creating || !title) ? 0.7 : 1
            }}
          >
            {creating ? (
              <>
                <span className="animate-spin" style={{ display: 'inline-block', width: '16px', height: '16px', border: '2px solid white', borderTopColor: 'transparent', borderRadius: '50%' }} />
                AI Decomposing Goal...
              </>
            ) : (
              <>
                <Plus size={18} /> Launch Project Space
              </>
            )}
          </button>
        </form>
      </div>

      {/* Projects List */}
      <div>
        <h3 style={{ fontSize: '1.25rem', fontWeight: 700, color: '#1e293b', marginBottom: '1.25rem' }}>Active Workspaces</h3>
        
        {loading ? (
          <div style={{ display: 'flex', justifyContent: 'center', padding: '3rem 0' }}>
            <span className="animate-spin" style={{ display: 'inline-block', width: '28px', height: '28px', border: '3px solid #6366f1', borderTopColor: 'transparent', borderRadius: '50%' }} />
          </div>
        ) : projects.length === 0 ? (
          <div style={{ background: '#f8fafc', border: '2px dashed #e2e8f0', borderRadius: '1.25rem', padding: '4rem 2rem', textAlign: 'center', color: '#64748b' }}>
            <Target size={48} style={{ color: '#cbd5e1', marginBottom: '1rem' }} />
            <p style={{ fontWeight: 500, color: '#475569', marginBottom: '0.25rem' }}>No active goals defined yet</p>
            <p style={{ fontSize: '0.875rem' }}>Enter a primary objective above to create your first accountability project space.</p>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            {projects.map((proj) => {
              const totalTasks = proj.tasks.length;
              const completedTasks = proj.tasks.filter(t => t.status === 'completed').length;
              const percent = totalTasks > 0 ? Math.round((completedTasks / totalTasks) * 100) : 0;

              return (
                <div
                  key={proj.id}
                  onClick={() => navigate(`/projects/${proj.id}`)}
                  style={{
                    background: 'white',
                    border: '1px solid #e2e8f0',
                    borderRadius: '1.25rem',
                    padding: '1.5rem',
                    boxShadow: '0 1px 3px 0 rgba(0, 0, 0, 0.05)',
                    cursor: 'pointer',
                    transition: 'all 0.2s',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '1rem'
                  }}
                  onMouseOver={(e) => {
                    e.currentTarget.style.borderColor = '#6366f1';
                    e.currentTarget.style.boxShadow = '0 10px 15px -3px rgba(99, 102, 241, 0.08)';
                  }}
                  onMouseOut={(e) => {
                    e.currentTarget.style.borderColor = '#e2e8f0';
                    e.currentTarget.style.boxShadow = '0 1px 3px 0 rgba(0, 0, 0, 0.05)';
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                    <div>
                      <h4 style={{ fontSize: '1.2rem', fontWeight: 700, color: '#0f172a', marginBottom: '0.25rem' }}>{proj.title}</h4>
                      <div style={{ display: 'flex', gap: '1rem', color: '#64748b', fontSize: '0.85rem' }}>
                        <span style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                          <Calendar size={14} /> Target: {new Date(proj.target_date).toLocaleDateString(undefined, { year: 'numeric', month: 'short' })}
                        </span>
                        <span>•</span>
                        <span>{totalTasks} milestones decomposed</span>
                      </div>
                    </div>
                    
                    <div style={{ display: 'flex', gap: '0.5rem' }}>
                      <button
                        onClick={(e) => handleDeleteProject(proj.id, e)}
                        style={{ border: 'none', background: 'transparent', color: '#94a3b8', cursor: 'pointer', padding: '0.25rem', borderRadius: '0.375rem', transition: 'color 0.2s' }}
                        onMouseOver={(e) => e.currentTarget.style.color = '#ef4444'}
                        onMouseOut={(e) => e.currentTarget.style.color = '#94a3b8'}
                      >
                        <Trash2 size={18} />
                      </button>
                      <div style={{ color: '#6366f1', padding: '0.25rem' }}>
                        <ArrowRight size={18} />
                      </div>
                    </div>
                  </div>

                  {/* Progress Indicator */}
                  <div>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.8rem', fontWeight: 600, color: '#475569', marginBottom: '0.375rem' }}>
                      <span>Milestones Execution Rate</span>
                      <span>{completedTasks}/{totalTasks} ({percent}%)</span>
                    </div>
                    <div style={{ height: '8px', background: '#f1f5f9', borderRadius: '4px', overflow: 'hidden' }}>
                      <div style={{ width: `${percent}%`, height: '100%', background: '#6366f1', transition: 'width 0.3s ease-out' }} />
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};
