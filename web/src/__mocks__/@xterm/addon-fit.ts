import { vi } from 'vitest';

// Mock FitAddon class
export class FitAddon {
  fit = vi.fn();
  proposeDimensions = vi.fn(() => ({ cols: 80, rows: 24 }));
  dispose = vi.fn();
}
