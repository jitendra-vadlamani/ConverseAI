import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { Login } from './Login';
import { WithAuth, fakeAuth } from '../test/auth';

function renderLogin(login = vi.fn().mockResolvedValue(undefined)) {
  render(
    <WithAuth value={fakeAuth({ user: null, login })}>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/" element={<div>chat home</div>} />
        </Routes>
      </MemoryRouter>
    </WithAuth>,
  );
  return login;
}

describe('Login', () => {
  it('logs in and goes to the chat', async () => {
    const login = renderLogin();
    await userEvent.type(screen.getByLabelText('Email'), 'me@example.com');
    await userEvent.type(screen.getByLabelText('Password'), 'password-123');
    await userEvent.click(screen.getByRole('button', { name: /login|sign in/i }));
    expect(login).toHaveBeenCalledWith('me@example.com', 'password-123');
    expect(await screen.findByText('chat home')).toBeInTheDocument();
  });

  it('shows the server message on failure and stays on the page', async () => {
    renderLogin(vi.fn().mockRejectedValue(new Error('invalid email or password')));
    await userEvent.type(screen.getByLabelText('Email'), 'me@example.com');
    await userEvent.type(screen.getByLabelText('Password'), 'nope');
    await userEvent.click(screen.getByRole('button', { name: /login|sign in/i }));
    expect(await screen.findByText('invalid email or password')).toBeInTheDocument();
    expect(screen.queryByText('chat home')).not.toBeInTheDocument();
  });
});
