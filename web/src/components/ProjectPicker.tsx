import { ProjectInfo, ProjectHealthStatus } from '../types';

type Props = {
  projects: ProjectInfo[];
  onSelect: (projectId: string) => void;
  onClose: () => void;
};

const getStatusClass = (status?: ProjectHealthStatus): string => {
  switch (status) {
    case 'online':
      return 'status-online';
    case 'degraded':
      return 'status-degraded';
    case 'offline':
      return 'status-offline';
    default:
      return 'status-unknown';
  }
};

const getStatusLabel = (status?: ProjectHealthStatus): string => {
  switch (status) {
    case 'online':
      return 'Online';
    case 'degraded':
      return 'Degraded';
    case 'offline':
      return 'Offline';
    default:
      return 'Unknown';
  }
};

const ProjectPicker = ({ projects, onSelect, onClose }: Props) => {
  return (
    <div className="project-picker-backdrop">
      <div className="project-picker">
        <div className="picker-header">
          <h3>Select Project</h3>
          <button onClick={onClose} aria-label="Close picker">
            ×
          </button>
        </div>
        {projects.length === 0 ? (
          <p className="picker-empty">No projects available.</p>
        ) : (
          <ul>
            {projects.map((project) => (
              <li key={project.id}>
                <button className="project-option" onClick={() => onSelect(project.id)}>
                  <div className="project-info">
                    <div className="project-header">
                      <strong>{project.displayName || project.id}</strong>
                      <span
                        className={`status-indicator ${getStatusClass(project.status)}`}
                        title={getStatusLabel(project.status)}
                      />
                    </div>
                    <p>{project.description}</p>
                  </div>
                  <span className="project-namespace">{project.namespace}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
};

export default ProjectPicker;
