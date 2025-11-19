import { ProjectInfo } from '../types';

type Props = {
  projects: ProjectInfo[];
  onSelect: (projectId: string) => void;
  onClose: () => void;
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
                  <div>
                    <strong>{project.displayName || project.id}</strong>
                    <p>{project.description}</p>
                  </div>
                  <span>{project.namespace}</span>
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
