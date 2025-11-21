import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { AuthProvider, useAuth } from './AuthContext';
import { ReactNode } from 'react';

describe('AuthContext', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    global.fetch = fetchMock;
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  const wrapper = ({ children }: { children: ReactNode }) => (
    <AuthProvider>{children}</AuthProvider>
  );

  describe('Initialization', () => {
    it('should start in checking state', () => {
      fetchMock.mockResolvedValue({
        ok: false,
        status: 401,
        json: async () => ({})
      });

      const { result } = renderHook(() => useAuth(), { wrapper });
      expect(result.current.authState).toBe('checking');
      expect(result.current.user).toBeNull();
    });

    it('should transition to authenticated when /api/auth/me succeeds', async () => {
      fetchMock.mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ user: { id: '123', username: 'testuser' } })
      });

      const { result } = renderHook(() => useAuth(), { wrapper });

      await waitFor(() => {
        expect(result.current.authState).toBe('authenticated');
      });

      expect(result.current.user).toEqual({ id: '123', username: 'testuser' });
    });

    it('should transition to authenticated when auth endpoints return 404 (disabled mode)', async () => {
      fetchMock.mockResolvedValue({
        ok: false,
        status: 404,
        json: async () => ({})
      });

      const { result } = renderHook(() => useAuth(), { wrapper });

      await waitFor(() => {
        expect(result.current.authState).toBe('authenticated');
      });

      expect(result.current.user).toBeNull();
    });

    it('should transition to authenticated when auth endpoints return 405 (disabled mode)', async () => {
      fetchMock.mockResolvedValue({
        ok: false,
        status: 405,
        json: async () => ({})
      });

      const { result } = renderHook(() => useAuth(), { wrapper });

      await waitFor(() => {
        expect(result.current.authState).toBe('authenticated');
      });

      expect(result.current.user).toBeNull();
    });

    it('should transition to unauthenticated when /api/auth/me fails', async () => {
      fetchMock.mockResolvedValue({
        ok: false,
        status: 401,
        json: async () => ({})
      });

      const { result } = renderHook(() => useAuth(), { wrapper });

      await waitFor(() => {
        expect(result.current.authState).toBe('unauthenticated');
      });

      expect(result.current.user).toBeNull();
    });

    it('should transition to unauthenticated when fetch throws', async () => {
      fetchMock.mockRejectedValue(new Error('Network error'));

      const { result } = renderHook(() => useAuth(), { wrapper });

      await waitFor(() => {
        expect(result.current.authState).toBe('unauthenticated');
      });

      expect(result.current.user).toBeNull();
    });
  });

  describe('useAuth hook', () => {
    it('should throw error when used outside provider', () => {
      // Suppress console.error for this test
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

      expect(() => {
        renderHook(() => useAuth());
      }).toThrow('useAuth must be used within an AuthProvider');

      consoleSpy.mockRestore();
    });
  });
});
