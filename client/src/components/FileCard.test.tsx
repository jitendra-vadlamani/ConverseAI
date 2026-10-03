import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FileCard } from './FileCard';

const fileID = 'user-u1/1791022981511962984-quarterly report.pdf';

describe('FileCard', () => {
  it('shows the original filename without the storage prefix', () => {
    render(<FileCard fileID={fileID} />);
    expect(screen.getByText('quarterly report.pdf')).toBeInTheDocument();
  });

  it('downloads through the API, never a direct storage URL', async () => {
    const open = vi.fn();
    vi.stubGlobal('open', open);
    render(<FileCard fileID={fileID} />);
    await userEvent.click(screen.getByText('Download'));
    expect(open).toHaveBeenCalledWith(
      '/api/chat/files/download?fileID=user-u1%2F1791022981511962984-quarterly%20report.pdf',
      '_blank',
      'noopener',
    );
  });

  it('only offers delete when a handler is given', async () => {
    const { rerender } = render(<FileCard fileID={fileID} />);
    expect(screen.queryByText('Delete')).not.toBeInTheDocument();
    const onDelete = vi.fn();
    vi.stubGlobal('open', vi.fn());
    rerender(<FileCard fileID={fileID} onDelete={onDelete} />);
    await userEvent.click(screen.getByText('Delete'));
    expect(onDelete).toHaveBeenCalledOnce();
    expect(window.open).not.toHaveBeenCalled(); // delete doesn't also download
  });
});
