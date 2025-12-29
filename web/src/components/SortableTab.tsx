import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';

interface SortableTabProps {
  id: string;
  children: React.ReactNode;
}

/**
 * SortableTab wraps a tab element to make it draggable within a sortable context.
 * Uses @dnd-kit's useSortable hook for drag-and-drop behavior.
 */
export function SortableTab({ id, children }: SortableTabProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    cursor: isDragging ? 'grabbing' : 'grab',
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={isDragging ? 'sortable-tab dragging' : 'sortable-tab'}
      {...attributes}
      {...listeners}
    >
      {children}
    </div>
  );
}

export default SortableTab;
