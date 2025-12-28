import { useRef, useState, useCallback, useEffect, ReactNode } from 'react';
import './SplitPane.css';

type SplitDirection = 'horizontal' | 'vertical';

type Props = {
  /** Direction of the split: 'horizontal' (left/right) or 'vertical' (top/bottom) */
  direction: SplitDirection;
  /** First pane content (left or top) */
  firstPane: ReactNode;
  /** Second pane content (right or bottom) */
  secondPane: ReactNode;
  /** Initial size of the first pane as percentage (0-100). Default: 50 */
  initialFirstPaneSize?: number;
  /** Minimum size of each pane in pixels. Default: 100 */
  minPaneSize?: number;
  /** Called when split ratio changes */
  onResize?: (firstPanePercent: number) => void;
  /** Custom class name for the container */
  className?: string;
};

/**
 * SplitPane - A resizable split view component
 *
 * Provides a draggable divider between two panes that can be arranged
 * horizontally (side-by-side) or vertically (top-bottom).
 */
const SplitPane = ({
  direction,
  firstPane,
  secondPane,
  initialFirstPaneSize = 50,
  minPaneSize = 100,
  onResize,
  className = '',
}: Props) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [firstPanePercent, setFirstPanePercent] = useState(initialFirstPaneSize);
  const [isDragging, setIsDragging] = useState(false);

  const isHorizontal = direction === 'horizontal';

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsDragging(true);
  }, []);

  const handleMouseMove = useCallback(
    (e: MouseEvent) => {
      if (!isDragging || !containerRef.current) return;

      const containerRect = containerRef.current.getBoundingClientRect();
      const containerSize = isHorizontal ? containerRect.width : containerRect.height;
      const position = isHorizontal
        ? e.clientX - containerRect.left
        : e.clientY - containerRect.top;

      // Calculate percentage, respecting minimum sizes
      const minPercent = (minPaneSize / containerSize) * 100;
      const maxPercent = 100 - minPercent;
      const newPercent = Math.min(maxPercent, Math.max(minPercent, (position / containerSize) * 100));

      setFirstPanePercent(newPercent);
      onResize?.(newPercent);
    },
    [isDragging, isHorizontal, minPaneSize, onResize]
  );

  const handleMouseUp = useCallback(() => {
    setIsDragging(false);
  }, []);

  // Attach/detach mouse event listeners for dragging
  useEffect(() => {
    if (isDragging) {
      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
      // Prevent text selection during drag
      document.body.style.userSelect = 'none';
      document.body.style.cursor = isHorizontal ? 'col-resize' : 'row-resize';
    }

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
    };
  }, [isDragging, handleMouseMove, handleMouseUp, isHorizontal]);

  // Dispatch resize events for child components (like Terminal/VNC)
  useEffect(() => {
    if (!isDragging) {
      // Delay to allow CSS to settle before resize
      const timeoutId = setTimeout(() => {
        window.dispatchEvent(new Event('resize'));
      }, 50);
      return () => clearTimeout(timeoutId);
    }
  }, [isDragging, firstPanePercent]);

  const containerClass = `split-pane split-pane--${direction} ${className} ${isDragging ? 'split-pane--dragging' : ''}`;

  return (
    <div ref={containerRef} className={containerClass}>
      <div
        className="split-pane__first"
        style={{
          [isHorizontal ? 'width' : 'height']: `${firstPanePercent}%`,
        }}
      >
        {firstPane}
      </div>
      <div
        className={`split-pane__divider split-pane__divider--${direction}`}
        onMouseDown={handleMouseDown}
        role="separator"
        aria-orientation={isHorizontal ? 'vertical' : 'horizontal'}
        aria-valuenow={Math.round(firstPanePercent)}
        aria-valuemin={0}
        aria-valuemax={100}
        tabIndex={0}
      >
        <div className="split-pane__divider-handle" />
      </div>
      <div
        className="split-pane__second"
        style={{
          [isHorizontal ? 'width' : 'height']: `${100 - firstPanePercent}%`,
        }}
      >
        {secondPane}
      </div>
    </div>
  );
};

export default SplitPane;
