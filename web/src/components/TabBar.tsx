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
} from '@dnd-kit/sortable';
import { ProjectInfo, TabMetrics } from '../types';
import MetricsIndicator from './MetricsIndicator';
import { SortableTab } from './SortableTab';

type TabBarProps = {
  tabs: Array<{ tabId: string; label: string; status: string; metrics?: TabMetrics; hasBellAlert?: boolean }>;
  activeTabId: string | null;
  onSelect: (tabId: string) => void;
  onClose: (tabId: string) => void;
  onNew: () => void;
  onReorder: (tabIds: string[]) => void;
  projects: ProjectInfo[];
};

const TabBar = ({ tabs, activeTabId, onSelect, onClose, onNew, onReorder, projects }: TabBarProps) => {
  // Configure sensors for pointer (mouse/touch) and keyboard accessibility
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 5, // 5px movement before drag starts (prevents accidental drags)
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    })
  );

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;

    if (over && active.id !== over.id) {
      const oldIndex = tabs.findIndex((tab) => tab.tabId === active.id);
      const newIndex = tabs.findIndex((tab) => tab.tabId === over.id);
      const newOrder = arrayMove(tabs, oldIndex, newIndex);
      onReorder(newOrder.map((tab) => tab.tabId));
    }
  };

  const tabIds = tabs.map((tab) => tab.tabId);

  return (
    <div className="tab-bar">
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={tabIds} strategy={horizontalListSortingStrategy}>
          <div className="tab-list">
            {tabs.map((tab) => (
              <SortableTab key={tab.tabId} id={tab.tabId}>
                <button
                  className={`tab-button ${tab.tabId === activeTabId ? 'active' : ''}`}
                  onClick={() => onSelect(tab.tabId)}
                >
                  <div className="tab-content">
                    <div className="tab-header">
                      <span className="tab-label">{tab.label}</span>
                      <span className={`tab-status tab-status-${tab.status}`}></span>
                      {tab.hasBellAlert && tab.tabId !== activeTabId && (
                        <span className="tab-bell-icon" title="Bell alert">🔔</span>
                      )}
                      <span
                        className="tab-close"
                        onClick={(event) => {
                          event.stopPropagation();
                          onClose(tab.tabId);
                        }}
                      >
                        ×
                      </span>
                    </div>
                    <MetricsIndicator metrics={tab.metrics} />
                  </div>
                </button>
              </SortableTab>
            ))}
            <button className="tab-button add" onClick={onNew} disabled={projects.length === 0}>
              +
            </button>
          </div>
        </SortableContext>
      </DndContext>
    </div>
  );
};

export default TabBar;
