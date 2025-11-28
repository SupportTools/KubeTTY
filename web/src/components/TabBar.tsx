import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragEndEvent,
} from '@dnd-kit/core';
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  horizontalListSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { ProjectInfo, ProjectLifecycleStatus, ProjectHealthStatus, TabMetrics } from '../types';

type TabData = {
  tabId: string;
  projectLabel: string;
  status: 'connecting' | 'connected' | 'reconnecting' | 'closed';
  metrics?: TabMetrics;
  projectNamespace?: string;
  lifecycleStatus?: ProjectLifecycleStatus;
  projectHealth?: ProjectHealthStatus;
  paused?: boolean;
  createdAt?: string;
  updatedAt?: string;
  lastError?: string;
};

type TabBarProps = {
  tabs: TabData[];
  activeTabId: string | null;
  onSelect: (tabId: string) => void;
  onClose: (tabId: string) => void;
  onNew: () => void;
  onReorder: (tabIds: string[]) => void;
  projects: ProjectInfo[];
};

type SortableTabProps = {
  tab: TabData;
  isActive: boolean;
  onSelect: (tabId: string) => void;
  onClose: (tabId: string) => void;
};

const formatDuration = (timestamp?: string): string => {
  if (!timestamp) return '—';
  const start = new Date(timestamp).getTime();
  if (Number.isNaN(start)) return '—';
  const diff = Date.now() - start;
  if (diff < 0) return 'Starting…';
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return '<1m';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
};

const formatStatusLabel = (status: TabData['status']) => {
  switch (status) {
    case 'connecting':
      return 'Connecting';
    case 'reconnecting':
      return 'Reconnecting';
    case 'closed':
      return 'Closed';
    default:
      return 'Connected';
  }
};

const formatPercent = (value?: number) => {
  if (value === undefined || Number.isNaN(value)) return '—';
  return `${Math.round(value)}%`;
};

const formatRate = (rx?: number, tx?: number) => {
  if (!rx && !tx) return '0 B/s';
  const total = (rx || 0) + (tx || 0);
  if (total === 0) return '0 B/s';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  let size = total;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[unitIndex]}`;
};

const MetricChip = ({ label, percent, value }: { label: string; percent?: number; value?: string }) => (
  <div className="tab-metric" title={value ? `${label} ${value}` : undefined}>
    <span className="tab-metric-label">{label}</span>
    <span className="tab-metric-value">{formatPercent(percent)}</span>
    <div className="tab-metric-bar">
      <div className="tab-metric-fill" style={{ width: `${Math.min(100, Math.max(0, percent || 0))}%` }} />
    </div>
  </div>
);

const SortableTab = ({ tab, isActive, onSelect, onClose }: SortableTabProps) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: tab.tabId });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    zIndex: isDragging ? 1000 : undefined,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`tab-card ${isActive ? 'active' : ''} ${isDragging ? 'dragging' : ''}`}
      onClick={() => onSelect(tab.tabId)}
      {...attributes}
      {...listeners}
    >
      <div className="tab-card-head">
        <div className="tab-card-title">
          <p className="tab-project-label">{tab.projectLabel}</p>
          <span className={`status-pill status-pill-${tab.status}`}>{formatStatusLabel(tab.status)}</span>
          {tab.metrics?.metadata && (
            <span className="tab-node" title={`Pod ${tab.metrics.metadata.podName} on ${tab.metrics.metadata.nodeName}`}>
              {tab.metrics.metadata.nodeName}
            </span>
          )}
        </div>
        <button
          className="tab-close"
          onClick={(event) => {
            event.stopPropagation();
            onClose(tab.tabId);
          }}
          aria-label={`Close ${tab.projectLabel}`}
        >
          ×
        </button>
      </div>
      <div className="tab-card-meta">
        <span>{tab.projectNamespace || 'Unknown namespace'}</span>
        <span className="meta-separator">•</span>
        <span>{formatDuration(tab.createdAt)}</span>
        {tab.lastError && <span className="tab-error">{tab.lastError}</span>}
      </div>
      <div className="tab-card-metrics">
        <MetricChip label="CPU" percent={tab.metrics?.cpu.percent} value={`${tab.metrics?.cpu.usage || 0}m`} />
        <MetricChip
          label="MEM"
          percent={tab.metrics?.memory.percent}
          value={`${Math.round((tab.metrics?.memory.usage || 0) / 1024 / 1024)} MB`}
        />
        <MetricChip
          label="DISK"
          percent={tab.metrics?.disk.percent}
          value={`${Math.round((tab.metrics?.disk.usage || 0) / 1024 / 1024)} MB`}
        />
        <div className="tab-metric network" title="Network throughput">
          <span className="tab-metric-label">NET</span>
          <span className="tab-metric-value">{formatRate(tab.metrics?.network.rxRate, tab.metrics?.network.txRate)}</span>
        </div>
      </div>
    </div>
  );
};

const TabBar = ({ tabs, activeTabId, onSelect, onClose, onNew, onReorder, projects }: TabBarProps) => {
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8, // Require 8px movement before starting drag
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;

    if (over && active.id !== over.id) {
      const oldIndex = tabs.findIndex((t) => t.tabId === active.id);
      const newIndex = tabs.findIndex((t) => t.tabId === over.id);

      const reorderedTabs = arrayMove(tabs, oldIndex, newIndex);
      onReorder(reorderedTabs.map((t) => t.tabId));
    }
  };

  return (
    <div className="tab-bar enhanced">
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={tabs.map((t) => t.tabId)}
          strategy={horizontalListSortingStrategy}
        >
          <div className="tab-list">
            {tabs.map((tab) => (
              <SortableTab
                key={tab.tabId}
                tab={tab}
                isActive={tab.tabId === activeTabId}
                onSelect={onSelect}
                onClose={onClose}
              />
            ))}
            <button className="tab-card add" onClick={onNew} disabled={projects.length === 0}>
              + New session
            </button>
          </div>
        </SortableContext>
      </DndContext>
    </div>
  );
};

export default TabBar;
