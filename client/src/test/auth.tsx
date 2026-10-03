import { vi } from 'vitest';
import type { ReactNode } from 'react';
import { AuthContext, type AuthContextType } from '../contexts/authContextValue';

// Wraps components in an AuthContext with overridable fakes.
export function fakeAuth(over: Partial<AuthContextType> = {}): AuthContextType {
  return {
    user: { id: 'u1', email: 'me@example.com', created_at: '2026-10-03T00:00:00Z' },
    loading: false,
    login: vi.fn().mockResolvedValue(undefined),
    register: vi.fn().mockResolvedValue(undefined),
    logout: vi.fn().mockResolvedValue(undefined),
    checkAuth: vi.fn().mockResolvedValue(undefined),
    ...over,
  };
}

export function WithAuth({ value, children }: { value: AuthContextType; children: ReactNode }) {
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
