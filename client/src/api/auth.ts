import type { User, AuthResponse } from '../types/auth';

export const loginApi = async (email: string, password: string): Promise<AuthResponse> => {
  const response = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });

  if (!response.ok) {
    const errorData = await response.text();
    throw new Error(errorData || 'Login failed');
  }

  return response.json();
};

export const registerApi = async (email: string, password: string): Promise<void> => {
  const response = await fetch('/api/auth/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });

  if (!response.ok) {
    const errorData = await response.text();
    throw new Error(errorData || 'Registration failed');
  }
};

export const logoutApi = async (): Promise<void> => {
  await fetch('/api/auth/logout', { method: 'POST' });
};

export const getMeApi = async (): Promise<User> => {
  const response = await fetch('/api/auth/me');
  if (!response.ok) {
    throw new Error('Not authenticated');
  }
  return response.json();
};

export const updatePasswordApi = async (oldPassword: string, newPassword: string): Promise<void> => {
  const response = await fetch('/api/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
  });

  if (!response.ok) {
    const errorData = await response.text();
    throw new Error(errorData || 'Failed to update password');
  }
};
