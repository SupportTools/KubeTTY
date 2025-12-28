import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { vi, describe, it, expect, afterEach, beforeEach } from 'vitest';

// Mock TerminalView
vi.mock('./TerminalView', () => ({
  default: ({ wsUrl, isFocused }: { wsUrl: string; isFocused: boolean }) => (
    <div data-testid="terminal-view" data-ws-url={wsUrl} data-focused={isFocused}>
      Mock Terminal
    </div>
  ),
}));

// Mock GUIView
vi.mock('./GUIView', () => ({
  default: ({ vncUrl, isFocused }: { vncUrl: string; isFocused: boolean }) => (
    <div data-testid="gui-view" data-vnc-url={vncUrl} data-focused={isFocused}>
      Mock GUI
    </div>
  ),
}));

// Mock SplitPane
vi.mock('./SplitPane', () => ({
  default: ({ firstPane, secondPane, direction }: { firstPane: React.ReactNode; secondPane: React.ReactNode; direction: string }) => (
    <div data-testid="split-pane" data-direction={direction}>
      <div data-testid="split-first">{firstPane}</div>
      <div data-testid="split-second">{secondPane}</div>
    </div>
  ),
}));

// Mock ViewToolbar
vi.mock('./ViewToolbar', () => ({
  default: ({ currentMode, onModeChange, guiEnabled }: { currentMode: string; onModeChange: (mode: string) => void; guiEnabled: boolean }) => (
    <div data-testid="view-toolbar" data-mode={currentMode} data-gui-enabled={guiEnabled}>
      <button data-testid="mode-terminal" onClick={() => onModeChange('terminal')}>Terminal</button>
      <button data-testid="mode-gui" onClick={() => onModeChange('gui')}>GUI</button>
      <button data-testid="mode-split-h" onClick={() => onModeChange('split-horizontal')}>Split H</button>
      <button data-testid="mode-split-v" onClick={() => onModeChange('split-vertical')}>Split V</button>
    </div>
  ),
}));

// Import component after mocks
import TabPane, { cleanupTabStorage } from './TabPane';

describe('TabPane', () => {
  const defaultProps = {
    tabId: 'test-tab-1',
    wsUrl: 'ws://localhost:8080/ws',
    healthUrl: '/api/healthz',
    isFocused: true,
  };

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  afterEach(() => {
    cleanup();
    localStorage.clear();
  });

  describe('Terminal-only mode (GUI disabled)', () => {
    it('renders terminal view when GUI is disabled', () => {
      render(<TabPane {...defaultProps} />);

      expect(screen.getByTestId('terminal-view')).toBeInTheDocument();
      expect(screen.queryByTestId('view-toolbar')).not.toBeInTheDocument();
    });

    it('does not render GUI view when GUI is disabled', () => {
      render(<TabPane {...defaultProps} />);

      expect(screen.queryByTestId('gui-view')).not.toBeInTheDocument();
    });

    it('passes wsUrl and focus state to terminal', () => {
      render(<TabPane {...defaultProps} />);

      const terminal = screen.getByTestId('terminal-view');
      expect(terminal).toHaveAttribute('data-ws-url', defaultProps.wsUrl);
      expect(terminal).toHaveAttribute('data-focused', 'true');
    });
  });

  describe('GUI-enabled mode', () => {
    const guiProject = {
      id: 'proj-1',
      name: 'test-project',
      displayName: 'Test Project',
      guiEnabled: true,
      guiVNCPort: 5900,
      guiResolution: '1280x720',
    };

    it('renders toolbar when GUI is enabled', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      expect(screen.getByTestId('view-toolbar')).toBeInTheDocument();
    });

    it('shows terminal view by default', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      expect(screen.getByTestId('terminal-view')).toBeInTheDocument();
    });

    it('switches to GUI view when mode changes', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.click(screen.getByTestId('mode-gui'));

      expect(screen.getByTestId('gui-view')).toBeInTheDocument();
      expect(screen.queryByTestId('terminal-view')).not.toBeInTheDocument();
    });

    it('switches to split-horizontal view', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.click(screen.getByTestId('mode-split-h'));

      const splitPane = screen.getByTestId('split-pane');
      expect(splitPane).toHaveAttribute('data-direction', 'horizontal');
      expect(screen.getByTestId('split-first')).toContainElement(screen.getByTestId('terminal-view'));
      expect(screen.getByTestId('split-second')).toContainElement(screen.getByTestId('gui-view'));
    });

    it('switches to split-vertical view', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.click(screen.getByTestId('mode-split-v'));

      const splitPane = screen.getByTestId('split-pane');
      expect(splitPane).toHaveAttribute('data-direction', 'vertical');
    });
  });

  describe('LocalStorage persistence', () => {
    const guiProject = {
      id: 'proj-1',
      name: 'test-project',
      displayName: 'Test Project',
      guiEnabled: true,
      guiVNCPort: 5900,
    };

    it('persists view mode to localStorage', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.click(screen.getByTestId('mode-gui'));

      expect(localStorage.getItem('kubetty-view-mode-test-tab-1')).toBe('gui');
    });

    it('restores view mode from localStorage', () => {
      localStorage.setItem('kubetty-view-mode-test-tab-1', 'gui');

      render(<TabPane {...defaultProps} project={guiProject} />);

      expect(screen.getByTestId('gui-view')).toBeInTheDocument();
    });

    it('restores split-horizontal mode from localStorage', () => {
      localStorage.setItem('kubetty-view-mode-test-tab-1', 'split-horizontal');

      render(<TabPane {...defaultProps} project={guiProject} />);

      expect(screen.getByTestId('split-pane')).toHaveAttribute('data-direction', 'horizontal');
    });
  });

  describe('cleanupTabStorage', () => {
    it('removes view mode from localStorage', () => {
      localStorage.setItem('kubetty-view-mode-test-tab', 'gui');
      localStorage.setItem('kubetty-split-ratio-test-tab', '60');

      cleanupTabStorage('test-tab');

      expect(localStorage.getItem('kubetty-view-mode-test-tab')).toBeNull();
      expect(localStorage.getItem('kubetty-split-ratio-test-tab')).toBeNull();
    });
  });

  describe('Keyboard shortcuts', () => {
    const guiProject = {
      id: 'proj-1',
      name: 'test-project',
      displayName: 'Test Project',
      guiEnabled: true,
      guiVNCPort: 5900,
    };

    it('switches to terminal mode with Ctrl+Shift+T', () => {
      localStorage.setItem('kubetty-view-mode-test-tab-1', 'gui');
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.keyDown(window, { key: 't', ctrlKey: true, shiftKey: true });

      expect(screen.getByTestId('terminal-view')).toBeInTheDocument();
    });

    it('switches to GUI mode with Ctrl+Shift+G', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.keyDown(window, { key: 'g', ctrlKey: true, shiftKey: true });

      expect(screen.getByTestId('gui-view')).toBeInTheDocument();
    });

    it('switches to split-horizontal with Ctrl+Shift+H', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.keyDown(window, { key: 'h', ctrlKey: true, shiftKey: true });

      expect(screen.getByTestId('split-pane')).toHaveAttribute('data-direction', 'horizontal');
    });

    it('switches to split-vertical with Ctrl+Shift+V', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      fireEvent.keyDown(window, { key: 'v', ctrlKey: true, shiftKey: true });

      expect(screen.getByTestId('split-pane')).toHaveAttribute('data-direction', 'vertical');
    });

    it('does not respond to shortcuts when not focused', () => {
      render(<TabPane {...defaultProps} project={guiProject} isFocused={false} />);

      fireEvent.keyDown(window, { key: 'g', ctrlKey: true, shiftKey: true });

      // Should still be in terminal mode
      expect(screen.getByTestId('terminal-view')).toBeInTheDocument();
    });

    it('does not respond to shortcuts without Ctrl+Shift', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      // Just Ctrl
      fireEvent.keyDown(window, { key: 'g', ctrlKey: true, shiftKey: false });
      expect(screen.getByTestId('terminal-view')).toBeInTheDocument();

      // Just Shift
      fireEvent.keyDown(window, { key: 'g', ctrlKey: false, shiftKey: true });
      expect(screen.getByTestId('terminal-view')).toBeInTheDocument();
    });
  });

  describe('GUI unavailable state', () => {
    const guiProjectNoPort = {
      id: 'proj-1',
      name: 'test-project',
      displayName: 'Test Project',
      guiEnabled: true,
      // No guiVNCPort
    };

    it('shows unavailable message when VNC port not configured', () => {
      render(<TabPane {...defaultProps} project={guiProjectNoPort} />);

      fireEvent.click(screen.getByTestId('mode-gui'));

      expect(screen.getByText(/GUI not available/i)).toBeInTheDocument();
    });
  });

  describe('Shortcuts hint', () => {
    const guiProject = {
      id: 'proj-1',
      name: 'test-project',
      displayName: 'Test Project',
      guiEnabled: true,
      guiVNCPort: 5900,
    };

    it('displays keyboard shortcuts hint when GUI enabled', () => {
      render(<TabPane {...defaultProps} project={guiProject} />);

      expect(screen.getByText(/Ctrl\+Shift/i)).toBeInTheDocument();
    });
  });
});
