import { render, RenderOptions } from '@testing-library/react';
import { ReactElement } from 'react';
import { AuthProvider } from './contexts/AuthContext';

// Custom render with AuthProvider wrapper
// Use this for components that depend on auth context
export function renderWithAuth(
  ui: ReactElement,
  options?: RenderOptions
) {
  return render(ui, { wrapper: AuthProvider, ...options });
}

// Re-export everything from React Testing Library
export * from '@testing-library/react';
export { userEvent } from '@testing-library/user-event';
