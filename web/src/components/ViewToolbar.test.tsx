import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { vi, describe, it, expect, afterEach } from 'vitest';
import ViewToolbar from './ViewToolbar';
import { ViewMode } from '../types';

describe('ViewToolbar', () => {
  const defaultProps = {
    currentMode: 'terminal' as ViewMode,
    onModeChange: vi.fn(),
    guiEnabled: true,
  };

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('renders nothing when guiEnabled is false', () => {
    const { container } = render(
      <ViewToolbar {...defaultProps} guiEnabled={false} />
    );

    expect(container.querySelector('.view-toolbar')).not.toBeInTheDocument();
  });

  it('renders toolbar when guiEnabled is true', () => {
    render(<ViewToolbar {...defaultProps} />);

    expect(document.querySelector('.view-toolbar')).toBeInTheDocument();
  });

  it('renders all view mode buttons', () => {
    render(<ViewToolbar {...defaultProps} />);

    // Should have 4 buttons for terminal, gui, split-horizontal, split-vertical
    const buttons = screen.getAllByRole('button');
    expect(buttons).toHaveLength(4);
  });

  it('marks current mode button as active', () => {
    render(<ViewToolbar {...defaultProps} currentMode="terminal" />);

    const buttons = screen.getAllByRole('button');
    const terminalButton = buttons[0];

    expect(terminalButton).toHaveClass('view-toolbar__button--active');
    expect(terminalButton).toHaveAttribute('aria-pressed', 'true');
  });

  it('calls onModeChange when button is clicked', () => {
    const onModeChange = vi.fn();
    render(<ViewToolbar {...defaultProps} onModeChange={onModeChange} />);

    const buttons = screen.getAllByRole('button');
    const guiButton = buttons[1]; // Second button is GUI mode

    fireEvent.click(guiButton);

    expect(onModeChange).toHaveBeenCalledWith('gui');
  });

  it('updates active button when currentMode changes', () => {
    const { rerender } = render(<ViewToolbar {...defaultProps} currentMode="terminal" />);

    let buttons = screen.getAllByRole('button');
    expect(buttons[0]).toHaveClass('view-toolbar__button--active'); // Terminal
    expect(buttons[1]).not.toHaveClass('view-toolbar__button--active'); // GUI

    // Change to GUI mode
    rerender(<ViewToolbar {...defaultProps} currentMode="gui" />);

    buttons = screen.getAllByRole('button');
    expect(buttons[0]).not.toHaveClass('view-toolbar__button--active'); // Terminal
    expect(buttons[1]).toHaveClass('view-toolbar__button--active'); // GUI
  });

  it('has correct aria-pressed for inactive buttons', () => {
    render(<ViewToolbar {...defaultProps} currentMode="terminal" />);

    const buttons = screen.getAllByRole('button');
    const guiButton = buttons[1];

    expect(guiButton).toHaveAttribute('aria-pressed', 'false');
  });

  it('buttons have title attributes for accessibility', () => {
    render(<ViewToolbar {...defaultProps} />);

    const buttons = screen.getAllByRole('button');

    expect(buttons[0]).toHaveAttribute('title', 'Terminal only view');
    expect(buttons[1]).toHaveAttribute('title', 'Desktop only view');
    expect(buttons[2]).toHaveAttribute('title', 'Side-by-side (terminal left, desktop right)');
    expect(buttons[3]).toHaveAttribute('title', 'Stacked (terminal top, desktop bottom)');
  });

  it('applies custom className', () => {
    render(<ViewToolbar {...defaultProps} className="custom-toolbar" />);

    expect(document.querySelector('.view-toolbar.custom-toolbar')).toBeInTheDocument();
  });

  it('split-horizontal mode is third button', () => {
    const onModeChange = vi.fn();
    render(<ViewToolbar {...defaultProps} onModeChange={onModeChange} />);

    const buttons = screen.getAllByRole('button');
    fireEvent.click(buttons[2]);

    expect(onModeChange).toHaveBeenCalledWith('split-horizontal');
  });

  it('split-vertical mode is fourth button', () => {
    const onModeChange = vi.fn();
    render(<ViewToolbar {...defaultProps} onModeChange={onModeChange} />);

    const buttons = screen.getAllByRole('button');
    fireEvent.click(buttons[3]);

    expect(onModeChange).toHaveBeenCalledWith('split-vertical');
  });

  it('renders icons for each button', () => {
    render(<ViewToolbar {...defaultProps} />);

    const icons = document.querySelectorAll('.view-toolbar__icon');
    expect(icons).toHaveLength(4);
  });

  it('renders labels for each button', () => {
    render(<ViewToolbar {...defaultProps} />);

    const labels = document.querySelectorAll('.view-toolbar__label');
    expect(labels).toHaveLength(4);
  });
});
