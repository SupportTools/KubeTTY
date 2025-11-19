import { ProjectInfo } from '../types';

type TabBarProps = {
  tabs: Array<{ tabId: string; label: string; status: string }>;
  activeTabId: string | null;
  onSelect: (tabId: string) => void;
  onClose: (tabId: string) => void;
  onNew: () => void;
  projects: ProjectInfo[];
};

const TabBar = ({ tabs, activeTabId, onSelect, onClose, onNew, projects }: TabBarProps) => {
  return (
    <div className="tab-bar">
      <div className="tab-list">
        {tabs.map((tab) => (
          <button
            key={tab.tabId}
            className={`tab-button ${tab.tabId === activeTabId ? 'active' : ''}`}
            onClick={() => onSelect(tab.tabId)}
          >
            <span className="tab-label">{tab.label}</span>
            <span className={`tab-status tab-status-${tab.status}`}></span>
            <span
              className="tab-close"
              onClick={(event) => {
                event.stopPropagation();
                onClose(tab.tabId);
              }}
            >
              ×
            </span>
          </button>
        ))}
        <button className="tab-button add" onClick={onNew} disabled={projects.length === 0}>
          +
        </button>
      </div>
    </div>
  );
};

export default TabBar;
