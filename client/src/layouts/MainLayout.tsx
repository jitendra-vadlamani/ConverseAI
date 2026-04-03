import React from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';
import { LogOut, Settings } from 'lucide-react';

interface MainLayoutProps {
  children: React.ReactNode;
  hidePadding?: boolean;
}

export const MainLayout: React.FC<MainLayoutProps> = ({ children, hidePadding }) => {
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  const handleLogout = async () => {
    await logout();
    navigate('/login');
  };

  return (
    <div className="app-container">
      <header className="app-header">
        <Link to="/" className="brand-link">
          <h1 className="brand-title">ConverseAI</h1>
        </Link>

        {user && (
          <div className="header-actions">
            <Link to="/settings" className="settings-link">
              <Settings size={20} />
            </Link>
            <button onClick={handleLogout} className="logout-btn" title="Logout">
              <LogOut size={20} />
            </button>
          </div>
        )}
      </header>
      <main className={`app-main ${hidePadding ? 'no-padding' : ''}`}>
        {children}
      </main>
    </div>
  );
};
