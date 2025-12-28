import { vi } from 'vitest';

// Mock Terminal class
export class Terminal {
  onData = vi.fn();
  onResize = vi.fn();
  onBell = vi.fn(() => ({ dispose: vi.fn() }));
  open = vi.fn();
  write = vi.fn();
  clear = vi.fn();
  reset = vi.fn();
  focus = vi.fn();
  blur = vi.fn();
  dispose = vi.fn();
  loadAddon = vi.fn();

  element: HTMLDivElement | null = null;
  rows = 24;
  cols = 80;

  constructor(public options: any = {}) {
    // Create a fake element
    this.element = document.createElement('div');
  }
}
