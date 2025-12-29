import { render, screen, fireEvent } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import TabBar from './TabBar';

// Mock @dnd-kit to avoid complex drag simulation
vi.mock('@dnd-kit/core', () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => <div data-testid="dnd-context">{children}</div>,
  closestCenter: vi.fn(),
  KeyboardSensor: vi.fn(),
  PointerSensor: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(() => []),
}));

vi.mock('@dnd-kit/sortable', () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => <div data-testid="sortable-context">{children}</div>,
  sortableKeyboardCoordinates: vi.fn(),
  horizontalListSortingStrategy: vi.fn(),
  arrayMove: vi.fn((arr, from, to) => {
    const result = [...arr];
    const [removed] = result.splice(from, 1);
    result.splice(to, 0, removed);
    return result;
  }),
}));

vi.mock('./SortableTab', () => ({
  SortableTab: ({ id, children }: { id: string; children: React.ReactNode }) => (
    <div data-testid={`sortable-tab-${id}`}>{children}</div>
  ),
}));

describe('TabBar', () => {
  const mockTabs = [
    { tabId: 'tab-1', label: 'Project A', status: 'connected' },
    { tabId: 'tab-2', label: 'Project B', status: 'connecting' },
    { tabId: 'tab-3', label: 'Project C', status: 'closed' },
  ];

  const mockProjects = [
    { name: 'project-a', displayName: 'Project A' },
    { name: 'project-b', displayName: 'Project B' },
  ];

  const defaultProps = {
    tabs: mockTabs,
    activeTabId: 'tab-1',
    onSelect: vi.fn(),
    onClose: vi.fn(),
    onNew: vi.fn(),
    onReorder: vi.fn(),
    projects: mockProjects,
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders all tabs', () => {
    render(<TabBar {...defaultProps} />);

    expect(screen.getByText('Project A')).toBeInTheDocument();
    expect(screen.getByText('Project B')).toBeInTheDocument();
    expect(screen.getByText('Project C')).toBeInTheDocument();
  });

  it('renders tabs in sortable wrappers', () => {
    render(<TabBar {...defaultProps} />);

    expect(screen.getByTestId('sortable-tab-tab-1')).toBeInTheDocument();
    expect(screen.getByTestId('sortable-tab-tab-2')).toBeInTheDocument();
    expect(screen.getByTestId('sortable-tab-tab-3')).toBeInTheDocument();
  });

  it('applies active class to selected tab', () => {
    render(<TabBar {...defaultProps} />);

    const activeTab = screen.getByText('Project A').closest('button');
    expect(activeTab).toHaveClass('active');

    const inactiveTab = screen.getByText('Project B').closest('button');
    expect(inactiveTab).not.toHaveClass('active');
  });

  it('calls onSelect when tab is clicked', () => {
    render(<TabBar {...defaultProps} />);

    fireEvent.click(screen.getByText('Project B'));
    expect(defaultProps.onSelect).toHaveBeenCalledWith('tab-2');
  });

  it('calls onClose when close button is clicked', () => {
    render(<TabBar {...defaultProps} />);

    const closeButtons = screen.getAllByText('×');
    fireEvent.click(closeButtons[1]); // Close second tab

    expect(defaultProps.onClose).toHaveBeenCalledWith('tab-2');
    expect(defaultProps.onSelect).not.toHaveBeenCalled(); // Should not select
  });

  it('calls onNew when add button is clicked', () => {
    render(<TabBar {...defaultProps} />);

    fireEvent.click(screen.getByText('+'));
    expect(defaultProps.onNew).toHaveBeenCalled();
  });

  it('disables add button when no projects available', () => {
    render(<TabBar {...defaultProps} projects={[]} />);

    const addButton = screen.getByText('+');
    expect(addButton).toBeDisabled();
  });

  it('renders DnD context wrapper', () => {
    render(<TabBar {...defaultProps} />);

    expect(screen.getByTestId('dnd-context')).toBeInTheDocument();
  });

  it('renders SortableContext wrapper', () => {
    render(<TabBar {...defaultProps} />);

    expect(screen.getByTestId('sortable-context')).toBeInTheDocument();
  });

  it('shows bell alert icon for tabs with alerts', () => {
    const tabsWithAlert = [
      { tabId: 'tab-1', label: 'Project A', status: 'connected' },
      { tabId: 'tab-2', label: 'Project B', status: 'connected', hasBellAlert: true },
    ];

    render(<TabBar {...defaultProps} tabs={tabsWithAlert} activeTabId="tab-1" />);

    // Bell should appear on inactive tab with alert
    expect(screen.getByTitle('Bell alert')).toBeInTheDocument();
  });

  it('does not show bell alert on active tab', () => {
    const tabsWithAlert = [
      { tabId: 'tab-1', label: 'Project A', status: 'connected', hasBellAlert: true },
    ];

    render(<TabBar {...defaultProps} tabs={tabsWithAlert} activeTabId="tab-1" />);

    // Bell should not appear on active tab even with hasBellAlert true
    expect(screen.queryByTitle('Bell alert')).not.toBeInTheDocument();
  });

  it('renders correct status indicator class', () => {
    render(<TabBar {...defaultProps} />);

    // Find status indicators by their classes
    const statusIndicators = document.querySelectorAll('.tab-status');
    expect(statusIndicators).toHaveLength(3);

    expect(statusIndicators[0]).toHaveClass('tab-status-connected');
    expect(statusIndicators[1]).toHaveClass('tab-status-connecting');
    expect(statusIndicators[2]).toHaveClass('tab-status-closed');
  });

  it('renders empty state when no tabs', () => {
    render(<TabBar {...defaultProps} tabs={[]} />);

    // Only the add button should be present
    expect(screen.getByText('+')).toBeInTheDocument();
    expect(screen.queryByText('Project A')).not.toBeInTheDocument();
  });
});
