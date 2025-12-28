import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { vi, describe, it, expect, afterEach } from 'vitest';
import SplitPane from './SplitPane';

describe('SplitPane', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders both panes', () => {
    render(
      <SplitPane
        direction="horizontal"
        firstPane={<div data-testid="first">First</div>}
        secondPane={<div data-testid="second">Second</div>}
      />
    );

    expect(screen.getByTestId('first')).toBeInTheDocument();
    expect(screen.getByTestId('second')).toBeInTheDocument();
  });

  it('renders horizontal layout correctly', () => {
    const { container } = render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    expect(container.querySelector('.split-pane--horizontal')).toBeInTheDocument();
    expect(container.querySelector('.split-pane__divider--horizontal')).toBeInTheDocument();
  });

  it('renders vertical layout correctly', () => {
    const { container } = render(
      <SplitPane
        direction="vertical"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    expect(container.querySelector('.split-pane--vertical')).toBeInTheDocument();
    expect(container.querySelector('.split-pane__divider--vertical')).toBeInTheDocument();
  });

  it('applies initial first pane size', () => {
    const { container } = render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
        initialFirstPaneSize={30}
      />
    );

    const firstPane = container.querySelector('.split-pane__first') as HTMLElement;
    expect(firstPane.style.width).toBe('30%');
  });

  it('applies custom class name', () => {
    const { container } = render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
        className="custom-class"
      />
    );

    expect(container.querySelector('.split-pane.custom-class')).toBeInTheDocument();
  });

  it('has accessible divider with separator role', () => {
    render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    const divider = screen.getByRole('separator');
    expect(divider).toBeInTheDocument();
    expect(divider).toHaveAttribute('aria-orientation', 'vertical');
  });

  it('has vertical aria-orientation for horizontal split', () => {
    render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    const divider = screen.getByRole('separator');
    expect(divider).toHaveAttribute('aria-orientation', 'vertical');
  });

  it('has horizontal aria-orientation for vertical split', () => {
    render(
      <SplitPane
        direction="vertical"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    const divider = screen.getByRole('separator');
    expect(divider).toHaveAttribute('aria-orientation', 'horizontal');
  });

  it('adds dragging class on mouse down', () => {
    const { container } = render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    const divider = screen.getByRole('separator');
    fireEvent.mouseDown(divider);

    expect(container.querySelector('.split-pane--dragging')).toBeInTheDocument();
  });

  it('removes dragging class on mouse up', () => {
    const { container } = render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    const divider = screen.getByRole('separator');
    fireEvent.mouseDown(divider);
    expect(container.querySelector('.split-pane--dragging')).toBeInTheDocument();

    fireEvent.mouseUp(document);
    expect(container.querySelector('.split-pane--dragging')).not.toBeInTheDocument();
  });

  it('calls onResize callback during drag', () => {
    const onResize = vi.fn();
    const { container } = render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
        onResize={onResize}
      />
    );

    const divider = screen.getByRole('separator');

    // Start dragging
    fireEvent.mouseDown(divider);

    // Simulate mouse move
    fireEvent.mouseMove(document, { clientX: 200, clientY: 100 });

    // End dragging
    fireEvent.mouseUp(document);

    // onResize should have been called
    expect(onResize).toHaveBeenCalled();
  });

  it('dispatches resize event after drag ends', () => {
    vi.useFakeTimers();
    const dispatchEventSpy = vi.spyOn(window, 'dispatchEvent');

    render(
      <SplitPane
        direction="horizontal"
        firstPane={<div>First</div>}
        secondPane={<div>Second</div>}
      />
    );

    const divider = screen.getByRole('separator');

    // Start and end drag
    fireEvent.mouseDown(divider);
    fireEvent.mouseUp(document);

    // Advance timers to trigger resize dispatch
    vi.advanceTimersByTime(100);

    // Should have dispatched resize event
    const resizeEvents = dispatchEventSpy.mock.calls.filter(
      (call) => (call[0] as Event).type === 'resize'
    );
    expect(resizeEvents.length).toBeGreaterThan(0);

    dispatchEventSpy.mockRestore();
    vi.useRealTimers();
  });
});
