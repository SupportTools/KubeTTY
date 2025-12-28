import { ViewMode } from '../types';
import './ViewToolbar.css';

type Props = {
  /** Current view mode */
  currentMode: ViewMode;
  /** Callback when view mode changes */
  onModeChange: (mode: ViewMode) => void;
  /** Whether GUI is available for this project */
  guiEnabled?: boolean;
  /** Optional class name */
  className?: string;
};

type ViewOption = {
  mode: ViewMode;
  label: string;
  shortLabel: string;
  icon: string;
  title: string;
};

const VIEW_OPTIONS: ViewOption[] = [
  {
    mode: 'terminal',
    label: 'Terminal',
    shortLabel: 'Term',
    icon: '>', // Terminal prompt icon
    title: 'Terminal only view',
  },
  {
    mode: 'gui',
    label: 'Desktop',
    shortLabel: 'GUI',
    icon: '[]', // Window/desktop icon
    title: 'Desktop only view',
  },
  {
    mode: 'split-horizontal',
    label: 'Split H',
    shortLabel: 'H',
    icon: '||', // Vertical divider icon
    title: 'Side-by-side (terminal left, desktop right)',
  },
  {
    mode: 'split-vertical',
    label: 'Split V',
    shortLabel: 'V',
    icon: '=', // Horizontal divider icon
    title: 'Stacked (terminal top, desktop bottom)',
  },
];

/**
 * ViewToolbar - Toggle between terminal, GUI, and split view modes
 *
 * Provides buttons to switch between different view layouts:
 * - Terminal: Full terminal view
 * - GUI: Full VNC desktop view
 * - Split Horizontal: Side-by-side terminal and desktop
 * - Split Vertical: Stacked terminal and desktop
 */
const ViewToolbar = ({
  currentMode,
  onModeChange,
  guiEnabled = false,
  className = '',
}: Props) => {
  if (!guiEnabled) {
    // If GUI is not enabled, don't render the toolbar
    return null;
  }

  return (
    <div className={`view-toolbar ${className}`}>
      <div className="view-toolbar__group">
        {VIEW_OPTIONS.map((option) => (
          <button
            key={option.mode}
            className={`view-toolbar__button ${
              currentMode === option.mode ? 'view-toolbar__button--active' : ''
            }`}
            onClick={() => onModeChange(option.mode)}
            title={option.title}
            aria-pressed={currentMode === option.mode}
          >
            <span className="view-toolbar__icon">{option.icon}</span>
            <span className="view-toolbar__label">{option.shortLabel}</span>
          </button>
        ))}
      </div>
    </div>
  );
};

export default ViewToolbar;
