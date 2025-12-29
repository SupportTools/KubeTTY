import { render, screen, cleanup, waitFor } from '@testing-library/react';
import { vi, describe, it, expect, afterEach, beforeEach } from 'vitest';

// Use vi.hoisted to create mocks that can be referenced in vi.mock
const mockFns = vi.hoisted(() => ({
  disconnect: vi.fn(),
  focus: vi.fn(),
  blur: vi.fn(),
  addEventListener: vi.fn(),
}));

// Mock must be defined before importing the component
vi.mock('@novnc/novnc/lib/rfb', () => {
  const MockRFB = vi.fn().mockImplementation(function (
    this: Record<string, unknown>,
    _target: HTMLElement,
    _url: string,
    _options?: Record<string, unknown>
  ) {
    this.viewOnly = false;
    this.scaleViewport = true;
    this.clipViewport = false;
    this.resizeSession = true;
    this.compressionLevel = 2;
    this.qualityLevel = 6;
    this.background = '';
    this.disconnect = mockFns.disconnect;
    this.focus = mockFns.focus;
    this.blur = mockFns.blur;
    this.addEventListener = mockFns.addEventListener;

    // Simulate successful connection after a short delay
    setTimeout(() => {
      const connectCall = mockFns.addEventListener.mock.calls.find(
        (call: [string, unknown]) => call[0] === 'connect'
      );
      if (connectCall && typeof connectCall[1] === 'function') {
        connectCall[1]({});
      }
    }, 10);
  });

  return { default: MockRFB };
});

// Import component after mock is set up
import GUIView from './GUIView';

describe('GUIView', () => {
  const defaultProps = {
    vncUrl: 'ws://localhost:5900/websockify',
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    // Restore real timers first - critical for React concurrent mode
    vi.useRealTimers();
    // Clear any pending timers before React cleanup
    vi.clearAllTimers();
    cleanup();
    vi.clearAllMocks();
  });

  it('renders the GUI view container', () => {
    render(<GUIView {...defaultProps} />);

    expect(document.querySelector('.gui-view')).toBeInTheDocument();
    expect(document.querySelector('.gui-view__canvas-container')).toBeInTheDocument();
  });

  it('renders status bar', () => {
    render(<GUIView {...defaultProps} />);

    expect(document.querySelector('.gui-view__status-bar')).toBeInTheDocument();
    expect(document.querySelector('.gui-view__status-indicator')).toBeInTheDocument();
  });

  it('shows connecting overlay after connection starts', async () => {
    // Component has a 50ms delay before connecting
    render(<GUIView {...defaultProps} />);

    // Wait for the connecting state (after 50ms delay)
    // Both assertions inside waitFor to avoid race with mock connection
    await waitFor(() => {
      expect(document.querySelector('.gui-view__overlay')).toBeInTheDocument();
      expect(document.querySelector('.gui-view__spinner')).toBeInTheDocument();
    });
  });

  it('calls onStatusChange when connection status changes', async () => {
    const onStatusChange = vi.fn();

    render(<GUIView {...defaultProps} onStatusChange={onStatusChange} />);

    // Wait for connection to start (after 50ms delay)
    await waitFor(() => {
      expect(onStatusChange).toHaveBeenCalledWith('connecting');
    });
  });

  it('applies custom VNC settings', () => {
    render(
      <GUIView
        {...defaultProps}
        viewOnly={true}
        scaleViewport={false}
        clipViewport={true}
        compressionLevel={5}
        qualityLevel={8}
      />
    );

    // Verify the component renders without errors
    expect(document.querySelector('.gui-view')).toBeInTheDocument();
  });

  it('cleans up RFB on unmount', async () => {
    const { unmount } = render(<GUIView {...defaultProps} />);

    // Wait for RFB to be created (after 50ms connection delay)
    await waitFor(() => {
      expect(mockFns.addEventListener).toHaveBeenCalled();
    });

    unmount();

    // Verify disconnect was called during cleanup
    expect(mockFns.disconnect).toHaveBeenCalled();
  });

  it('renders with correct status indicator class', async () => {
    render(<GUIView {...defaultProps} />);

    // Initially disconnected, then transitions to connecting after 50ms delay
    const indicator = document.querySelector('.gui-view__status-indicator');
    expect(indicator).toHaveClass('gui-view__status-indicator--disconnected');

    // Wait for connecting state
    await waitFor(() => {
      expect(indicator).toHaveClass('gui-view__status-indicator--connecting');
    });
  });

  it('handles focus changes', async () => {
    const { rerender } = render(<GUIView {...defaultProps} isFocused={true} />);

    // Wait for addEventListener to be called
    await waitFor(() => {
      expect(mockFns.addEventListener).toHaveBeenCalled();
    });

    // Rerender with focus false
    rerender(<GUIView {...defaultProps} isFocused={false} />);

    // Focus/blur behavior depends on RFB instance being available
    expect(document.querySelector('.gui-view')).toBeInTheDocument();
  });

  it('displays status message', async () => {
    render(<GUIView {...defaultProps} />);

    // Wait for connection to start (after 50ms delay) - message is in overlay
    await waitFor(() => {
      const message = document.querySelector('.gui-view__message');
      expect(message).toBeInTheDocument();
    });

    const message = document.querySelector('.gui-view__message');
    expect(message?.textContent).toContain('Connecting');
  });

  it('registers event listeners on connection', async () => {
    render(<GUIView {...defaultProps} />);

    await waitFor(() => {
      expect(mockFns.addEventListener).toHaveBeenCalledWith('connect', expect.any(Function));
      expect(mockFns.addEventListener).toHaveBeenCalledWith('disconnect', expect.any(Function));
      expect(mockFns.addEventListener).toHaveBeenCalledWith('credentialsrequired', expect.any(Function));
      expect(mockFns.addEventListener).toHaveBeenCalledWith('securityfailure', expect.any(Function));
    });
  });
});
