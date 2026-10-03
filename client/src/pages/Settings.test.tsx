import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api/auth', () => ({
  updatePasswordApi: vi.fn(),
  deleteAccountApi: vi.fn(),
}));

import { updatePasswordApi, deleteAccountApi } from '../api/auth';
import { Settings } from './Settings';
import { WithAuth, fakeAuth } from '../test/auth';

function renderSettings() {
  return render(<WithAuth value={fakeAuth()}><Settings /></WithAuth>);
}

async function openSecurity() {
  await userEvent.click(screen.getByRole('button', { name: /Security/ }));
}

beforeEach(() => {
  vi.mocked(updatePasswordApi).mockReset();
  vi.mocked(deleteAccountApi).mockReset();
});

describe('Settings', () => {
  it('shows the signed-in email', () => {
    renderSettings();
    expect(screen.getByDisplayValue('me@example.com')).toBeInTheDocument();
  });

  it('refuses mismatched new passwords without calling the server', async () => {
    renderSettings();
    await openSecurity();
    await userEvent.type(screen.getByLabelText('Current Password', { selector: '#oldPassword' }), 'old-password');
    await userEvent.type(screen.getByLabelText('New Password'), 'new-password-1');
    await userEvent.type(screen.getByLabelText('Confirm New Password'), 'new-password-2');
    await userEvent.click(screen.getByRole('button', { name: 'Update Password' }));
    expect(screen.getByText('New passwords do not match')).toBeInTheDocument();
    expect(updatePasswordApi).not.toHaveBeenCalled();
  });

  it('changes the password and shows server errors', async () => {
    vi.mocked(updatePasswordApi).mockRejectedValueOnce(new Error('invalid email or password'));
    renderSettings();
    await openSecurity();
    await userEvent.type(screen.getByLabelText('Current Password', { selector: '#oldPassword' }), 'wrong');
    await userEvent.type(screen.getByLabelText('New Password'), 'new-password-1');
    await userEvent.type(screen.getByLabelText('Confirm New Password'), 'new-password-1');
    await userEvent.click(screen.getByRole('button', { name: 'Update Password' }));
    expect(await screen.findByText('invalid email or password')).toBeInTheDocument();
    expect(updatePasswordApi).toHaveBeenCalledWith('wrong', 'new-password-1');

    vi.mocked(updatePasswordApi).mockResolvedValueOnce();
    await userEvent.click(screen.getByRole('button', { name: 'Update Password' }));
    expect(await screen.findByText('Password updated successfully!')).toBeInTheDocument();
  });

  it('deletes the account only after confirmation and with the password', async () => {
    const confirm = vi.fn(() => false);
    vi.stubGlobal('confirm', confirm);
    renderSettings();
    await openSecurity();
    await userEvent.type(screen.getByLabelText('Current Password', { selector: '#deletePassword' }), 'password-123');
    const button = screen.getByRole('button', { name: 'Delete my account and data' });

    await userEvent.click(button);
    expect(confirm).toHaveBeenCalled();
    expect(deleteAccountApi).not.toHaveBeenCalled();

    confirm.mockReturnValue(true);
    vi.mocked(deleteAccountApi).mockRejectedValueOnce(new Error('invalid email or password'));
    await userEvent.click(button);
    expect(deleteAccountApi).toHaveBeenCalledWith('password-123');
    expect(await screen.findByText('invalid email or password')).toBeInTheDocument();
    await waitFor(() => expect(button).not.toBeDisabled());
  });
});
