import React, { useState } from 'react';
import { updatePasswordApi } from '../api/auth';
import { useAuth } from '../hooks/useAuth';
import { 
  Lock, 
  User as UserIcon, 
  Shield, 
  Loader2, 
  CheckCircle2, 
  AlertCircle
} from 'lucide-react';

type TabType = 'profile' | 'security';

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
    }
  };

  return (
    <div className="settings-layout">
      <aside className="settings-sidebar">
        <button className={`sidebar-item ${activeTab === 'profile' ? 'active' : ''}`} onClick={() => setActiveTab('profile')}><UserIcon size={20} /> Profile</button>
        <button className={`sidebar-item ${activeTab === 'security' ? 'active' : ''}`} onClick={() => setActiveTab('security')}><Shield size={20} /> Security</button>
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
