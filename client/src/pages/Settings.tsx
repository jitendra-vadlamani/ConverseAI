import React, { useState, useEffect } from 'react';
import { updatePasswordApi } from '../api/auth';
import { listLLMsApi, addLLMApi, deleteLLMApi } from '../api/llm';
import type { LLMInfo, LLMConfig } from '../api/llm';
import { useAuth } from '../hooks/useAuth';
import { 
  Lock, 
  User as UserIcon, 
  Bell, 
  Shield, 
  Cpu, 
  Loader2, 
  CheckCircle2, 
  AlertCircle, 
  Plus, 
  Trash2,
  Database
} from 'lucide-react';

type TabType = 'profile' | 'security' | 'models' | 'notifications';

export const Settings: React.FC = () => {
  const { user } = useAuth();
  const [activeTab, setActiveTab] = useState<TabType>('profile');

  // Password Update State
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [loading, setLoading] = useState(false);

  // LLM Models State
  const [models, setModels] = useState<LLMInfo[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [showAddForm, setShowAddForm] = useState(false);
  const [newModel, setNewModel] = useState<Partial<LLMConfig>>({
    provider: 'openai',
    name: '',
    model_name: '',
    base_url: '',
    api_key: '',
    description: '',
    context_window: 4096,
  });

  const fetchModels = async () => {
    setLoadingModels(true);
    try {
      const data = await listLLMsApi();
      setModels(data);
    } catch (err) {
      console.error('Failed to fetch models:', err);
    } finally {
      setLoadingModels(false);
    }
  };

  useEffect(() => {
    if (activeTab === 'models') {
      fetchModels();
    }
  }, [activeTab]);

  const handlePasswordUpdate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSuccess('');
    if (newPassword !== confirmPassword) {
      setError('New passwords do not match');
      return;
    }
    setLoading(true);
    try {
      await updatePasswordApi(oldPassword, newPassword);
      setSuccess('Password updated successfully!');
      setOldPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (err: any) {
      setError(err.message || 'Failed to update password');
    } finally {
      setLoading(false);
    }
  };

  const handleAddModel = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    try {
      await addLLMApi(newModel as LLMConfig);
      setShowAddForm(false);
      setNewModel({ provider: 'openai', name: '', model_name: '', base_url: '', api_key: '', description: '', context_window: 4096 });
      fetchModels();
    } catch (err: any) {
      setError(err.message || 'Failed to add model');
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteModel = async (id: string) => {
    if (!window.confirm('Are you sure you want to remove this model configuration?')) return;
    try {
      await deleteLLMApi(id);
      fetchModels();
    } catch (err: any) {
      alert(err.message || 'Failed to delete model');
    }
  };

  const renderTabContent = () => {
    switch (activeTab) {
      case 'profile':
        return (
          <div>
            <div className="tab-header">
              <h2>Profile Details</h2>
              <p>Manage your public information and account identification.</p>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', maxWidth: '500px' }}>
              <div className="form-group">
                <label>Email Address</label>
                <div className="input-wrapper">
                  <UserIcon className="input-icon" />
                  <input type="text" value={user?.email || ''} readOnly disabled />
                </div>
              </div>
              <div className="form-group">
                <label>Account ID</label>
                <input type="text" value={user?.id || ''} readOnly disabled className="readonly-input" />
              </div>
            </div>
          </div>
        );
      case 'security':
        return (
          <div>
            <div className="tab-header">
              <h2>Password & Security</h2>
              <p>Update your password to keep your account secure.</p>
            </div>
            {error && <div className="auth-error"><AlertCircle size={18} /> {error}</div>}
            {success && <div className="auth-success"><CheckCircle2 size={18} /> {success}</div>}
            <form onSubmit={handlePasswordUpdate} style={{ maxWidth: '400px' }}>
              <div className="form-group">
                <label htmlFor="oldPassword">Current Password</label>
                <div className="input-wrapper"><Lock className="input-icon" /><input id="oldPassword" type="password" value={oldPassword} onChange={(e) => setOldPassword(e.target.value)} placeholder="••••••••" required /></div>
              </div>
              <div className="form-group">
                <label htmlFor="newPassword">New Password</label>
                <div className="input-wrapper"><Lock className="input-icon" /><input id="newPassword" type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="••••••••" required /></div>
              </div>
              <div className="form-group">
                <label htmlFor="confirmPassword">Confirm New Password</label>
                <div className="input-wrapper"><Lock className="input-icon" /><input id="confirmPassword" type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} placeholder="••••••••" required /></div>
              </div>
              <button type="submit" disabled={loading} className="auth-button">
                {loading ? <><Loader2 className="animate-spin" size={18} /> Updating...</> : 'Update Password'}
              </button>
            </form>
          </div>
        );
      case 'models':
        return (
          <div>
            <div className="tab-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div>
                <h2>AI Models</h2>
                <p>Configure local Ollama models and external cloud providers.</p>
              </div>
              <button onClick={() => setShowAddForm(!showAddForm)} className="add-btn">
                <Plus size={18} /> {showAddForm ? 'Cancel' : 'Add Model'}
              </button>
            </div>

            {showAddForm ? (
              <div className="model-form-card">
                <form onSubmit={handleAddModel}>
                  <div className="form-row">
                    <div className="form-group">
                      <label>Provider</label>
                      <select value={newModel.provider} onChange={(e) => setNewModel({...newModel, provider: e.target.value as any})} className="select-input">
                        <option value="openai">OpenAI</option>
                        <option value="claude">Claude</option>
                        <option value="ollama">Ollama (Custom)</option>
                        <option value="custom">Custom (OpenAI Compatible)</option>
                      </select>
                    </div>
                    <div className="form-group" style={{ flex: 2 }}>
                      <label>Friendly Name</label>
                      <input type="text" value={newModel.name} onChange={(e) => setNewModel({...newModel, name: e.target.value})} placeholder="e.g. My ChatGPT" required />
                    </div>
                  </div>
                  <div className="form-row">
                    <div className="form-group">
                      <label>Model Identifier</label>
                      <input type="text" value={newModel.model_name} onChange={(e) => setNewModel({...newModel, model_name: e.target.value})} placeholder="e.g. gpt-4" required />
                    </div>
                    <div className="form-group" style={{ flex: 2 }}>
                      <label>Base URL (Optional)</label>
                      <input type="text" value={newModel.base_url} onChange={(e) => setNewModel({...newModel, base_url: e.target.value})} placeholder="https://api.openai.com/v1" />
                    </div>
                  </div>
                  <div className="form-row">
                    <div className="form-group">
                      <label>Context Window</label>
                      <input type="number" value={newModel.context_window || 4096} onChange={(e) => setNewModel({...newModel, context_window: parseInt(e.target.value) || 0})} placeholder="e.g. 4096" />
                    </div>
                    <div className="form-group" style={{ flex: 2 }}>
                      <label>Description (Optional)</label>
                      <input type="text" value={newModel.description} onChange={(e) => setNewModel({...newModel, description: e.target.value})} placeholder="Briefly describe this model..." />
                    </div>
                  </div>
                  <div className="form-group">
                    <label>API Key</label>
                    <div className="input-wrapper">
                      <Shield className="input-icon" />
                      <input type="password" value={newModel.api_key} onChange={(e) => setNewModel({...newModel, api_key: e.target.value})} placeholder="sk-..." required={newModel.provider !== 'ollama'} />
                    </div>
                  </div>
                  <button type="submit" disabled={loading} className="auth-button">Add Model</button>
                </form>
              </div>
            ) : (
              <div style={{ minHeight: '300px' }}>
                {loadingModels ? (
                  <div className="loading-state"><Loader2 className="animate-spin" /> Fetching models...</div>
                ) : models && models.length > 0 ? (
                  <div className="models-grid">
                    {models.map((llm) => (
                      <div key={llm.config.id || llm.config.model_name} className="model-item">
                        <div className="model-header">
                          <span className={`status-badge ${llm.status.toLowerCase()}`}></span>
                          <span className="model-name">{llm.config.name}</span>
                          {llm.is_system && <span className="system-tag">System</span>}
                        </div>
                        <div className="model-details">
                          <p><strong>Provider:</strong> {llm.config.provider}</p>
                          <p><strong>Model:</strong> {llm.config.model_name}</p>
                          <p className={`status-text ${llm.status.toLowerCase()}`}>Status: {llm.status}</p>
                        </div>
                        {!llm.is_system && (
                          <button 
                            className="delete-model-btn"
                            onClick={() => handleDeleteModel(llm.config.id!)}
                          >
                            <Trash2 size={16} />
                          </button>
                        )}
                        {llm.config.description && <p className="model-desc">{llm.config.description}</p>}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="empty-state">
                    <Database size={48} className="empty-icon" />
                    <h3>No Models Configured</h3>
                    <p>Add a local Ollama model or a cloud provider to start chatting.</p>
                    <button className="add-first-btn" onClick={() => setShowAddForm(true)}>
                      <Plus size={18} /> Add Your First Model
                    </button>
                  </div>
                )}
              </div>
            )}
          </div>
        );
      case 'notifications':
        return (
          <div><div className="tab-header"><h2>Notifications</h2><p>Manage alerts.</p></div><p>Coming soon.</p></div>
        );
    }
  };

  return (
    <div className="settings-layout">
      <aside className="settings-sidebar">
        <button className={`sidebar-item ${activeTab === 'profile' ? 'active' : ''}`} onClick={() => setActiveTab('profile')}><UserIcon size={20} /> Profile</button>
        <button className={`sidebar-item ${activeTab === 'security' ? 'active' : ''}`} onClick={() => setActiveTab('security')}><Shield size={20} /> Security</button>
        <button className={`sidebar-item ${activeTab === 'models' ? 'active' : ''}`} onClick={() => setActiveTab('models')}><Cpu size={20} /> Models</button>
        <button className={`sidebar-item ${activeTab === 'notifications' ? 'active' : ''}`} onClick={() => setActiveTab('notifications')}><Bell size={20} /> Notifications</button>
      </aside>
      <main className="tab-content">{renderTabContent()}</main>
      <style>{`
        .settings-layout {
          display: flex;
          gap: 2rem;
          max-width: 1200px;
          margin: 0 auto;
          padding: 2rem;
          height: 100%;
        }
        .settings-sidebar {
          width: 240px;
          display: flex;
          flex-direction: column;
          gap: 0.5rem;
          flex-shrink: 0;
        }
        .sidebar-item {
          display: flex;
          align-items: center;
          gap: 0.75rem;
          padding: 0.875rem 1.25rem;
          background: transparent;
          border: 1px solid transparent;
          border-radius: 0.75rem;
          color: #64748b;
          font-size: 0.95rem;
          font-weight: 500;
          cursor: pointer;
          transition: all 0.2s;
          text-align: left;
          width: 100%;
        }
        .sidebar-item:hover {
          background: #f1f5f9;
          color: #1e293b;
        }
        .sidebar-item.active {
          background: white;
          color: #6366f1;
          border-color: #e2e8f0;
          box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05);
        }
        .tab-content {
          flex: 1;
          background: white;
          border-radius: 1.5rem;
          padding: 2.5rem;
          border: 1px solid #e2e8f0;
          box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.05);
          min-height: 500px;
        }
        .tab-header {
          margin-bottom: 2.5rem;
        }
        .tab-header h2 {
          font-size: 1.5rem;
          font-weight: 700;
          color: #0f172a;
          margin-bottom: 0.5rem;
        }
        .tab-header p {
          color: #64748b;
          font-size: 0.95rem;
        }
        .readonly-input { background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 0.75rem; padding: 0.625rem 1rem; font-size: 0.9rem; color: #64748b; width: 100%; font-family: monospace; }
        .add-btn { background: #6366f1; color: white; border: none; padding: 0.5rem 1rem; border-radius: 0.5rem; font-size: 0.85rem; font-weight: 600; display: flex; align-items: center; gap: 0.5rem; cursor: pointer; transition: all 0.2s; }
        .add-btn:hover { background: #4f46e5; transform: translateY(-1px); }
        .models-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 1rem; }
        .model-item { border: 1px solid #e2e8f0; border-radius: 0.75rem; padding: 1.25rem; transition: all 0.2s; position: relative; background: white; }
        .model-item:hover { border-color: #6366f1; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05); }
        .model-header { display: flex; items-center: flex-start; gap: 0.75rem; margin-bottom: 0.75rem; }
        .model-name { font-weight: 600; font-size: 1rem; color: #1e293b; }
        .system-tag { font-size: 0.65rem; background: #f1f5f9; color: #64748b; padding: 0.1rem 0.4rem; border-radius: 0.25rem; font-weight: 600; text-transform: uppercase; }
        .status-badge { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
        .status-badge.online { background: #10b981; box-shadow: 0 0 0 2px #ecfdf5; }
        .status-badge.offline { background: #ef4444; box-shadow: 0 0 0 2px #fef2f2; }
        .status-badge.missing { background: #f59e0b; box-shadow: 0 0 0 2px #fffbeb; }
        .model-details { font-size: 0.85rem; color: #64748b; display: flex; flex-direction: column; gap: 0.25rem; }
        .status-text { font-weight: 600; }
        .status-text.online { color: #059669; }
        .status-text.offline { color: #dc2626; }
        .status-text.missing { color: #d97706; }
        .delete-model-btn { position: absolute; top: 1.25rem; right: 1.25rem; color: #94a3b8; background: none; border: none; padding: 0.4rem; cursor: pointer; transition: all 0.2s; border-radius: 0.375rem; }
        .delete-model-btn:hover { color: #ef4444; background: #fef2f2; }
        .empty-state { text-align: center; padding: 4rem 2rem; border: 2px dashed #e2e8f0; border-radius: 1rem; display: flex; flex-direction: column; align-items: center; gap: 1rem; color: #64748b; }
        .empty-icon { color: #cbd5e1; }
        .empty-state h3 { font-size: 1.25rem; color: #1e293b; margin: 0; }
        .empty-state p { margin: 0; font-size: 0.95rem; max-width: 300px; line-height: 1.5; }
        .add-first-btn { background: #6366f1; color: white; border: none; padding: 0.75rem 1.5rem; border-radius: 0.75rem; font-weight: 600; cursor: pointer; display: flex; align-items: center; gap: 0.5rem; transition: all 0.2s; margin-top: 0.5rem; }
        .add-first-btn:hover { background: #4f46e5; transform: scale(1.02); }
        .loading-state { text-align: center; padding: 2rem; color: #64748b; display: flex; align-items: center; justify-content: center; gap: 0.75rem; }
      `}</style>
    </div>
  );
};
