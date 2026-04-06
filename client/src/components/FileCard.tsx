import React from 'react';
import { FileIcon, Download, ExternalLink, Trash2 } from 'lucide-react';
import { getPresignedUrlApi } from '../api/chat';

interface FileCardProps {
  fileID: string;
  compact?: boolean;
  onDelete?: () => void;
}

export const FileCard: React.FC<FileCardProps> = ({ fileID, compact = false, onDelete }) => {
  // Extract filename from fileID (format: user-id/timestamp-filename.ext)
  const getDisplayFilename = (id: string) => {
    const parts = id.split('/');
    const filenameWithTimestamp = parts[parts.length - 1];
    const firstDash = filenameWithTimestamp.indexOf('-');
    if (firstDash !== -1) {
      return filenameWithTimestamp.substring(firstDash + 1);
    }
    return filenameWithTimestamp;
  };

  const handleDownload = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      const url = await getPresignedUrlApi(fileID);
      window.open(url, '_blank');
    } catch (err) {
      alert('Failed to generate download URL');
    }
  };

  const handleDelete = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onDelete) onDelete();
  };

  const filename = getDisplayFilename(fileID);

  if (compact) {
    return (
      <div 
        className="file-card compact" 
        onClick={handleDownload}
        title={`Download ${filename}`}
      >
        <FileIcon size={14} />
        <span className="file-name">{filename}</span>
        <div className="compact-actions">
          <Download size={12} className="download-icon" />
          {onDelete && (
            <button className="delete-btn-icon" onClick={handleDelete} title="Delete">
              <Trash2 size={12} />
            </button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="file-card" onClick={handleDownload}>
      <div className="file-icon-wrapper">
        <FileIcon size={20} />
      </div>
      <div className="file-info">
        <div className="file-name" title={filename}>{filename}</div>
        <div className="file-actions">
          <div className="action-link" onClick={handleDownload}>
            <span className="action-text">Download</span>
            <Download size={12} />
          </div>
          {onDelete && (
            <div className="action-link delete" onClick={handleDelete}>
              <span className="action-text">Delete</span>
              <Trash2 size={12} />
            </div>
          )}
        </div>
      </div>
      <style>{`
        .file-card {
          display: flex;
          align-items: center;
          gap: 0.75rem;
          padding: 0.625rem;
          background: #f8fafc;
          border: 1px solid #e2e8f0;
          border-radius: 0.5rem;
          cursor: pointer;
          transition: all 0.2s;
          max-width: 240px;
          margin-top: 0.5rem;
        }
        .file-card:hover {
          background: #f1f5f9;
          border-color: #cbd5e1;
          box-shadow: 0 1px 2px rgba(0,0,0,0.05);
        }
        .file-icon-wrapper {
          width: 36px;
          height: 36px;
          background: white;
          border-radius: 0.375rem;
          display: flex;
          align-items: center;
          justify-content: center;
          color: #64748b;
          border: 1px solid #f1f5f9;
        }
        .file-info {
          flex: 1;
          min-width: 0;
        }
        .file-name {
          font-size: 0.8125rem;
          font-weight: 600;
          color: #1e293b;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }
        .file-actions {
          display: flex;
          align-items: center;
          gap: 0.75rem;
          margin-top: 0.125rem;
        }
        .action-link {
          display: flex;
          align-items: center;
          gap: 0.25rem;
          font-size: 0.6875rem;
          color: #6366f1;
          font-weight: 500;
          cursor: pointer;
        }
        .action-link:hover {
          text-decoration: underline;
        }
        .action-link.delete {
          color: #f43f5e;
        }
        
        .compact-actions {
          display: flex;
          align-items: center;
          gap: 0.375rem;
        }
        .delete-btn-icon {
          background: none;
          border: none;
          padding: 0;
          display: flex;
          align-items: center;
          color: #94a3b8;
          cursor: pointer;
        }
        .delete-btn-icon:hover {
          color: #f43f5e;
        }
        
        .file-card.compact {
          display: inline-flex;
          padding: 0.25rem 0.625rem;
          gap: 0.5rem;
          margin-top: 0;
          border-radius: 1rem;
          background: #eff6ff;
          border-color: #dbeafe;
          color: #2563eb;
          max-width: 100%;
        }
        .file-card.compact:hover {
          background: #dbeafe;
        }
        .file-card.compact .file-name {
          color: #2563eb;
          font-size: 0.75rem;
        }
        .download-icon {
          opacity: 0.6;
        }
      `}</style>
    </div>
  );
};
