-- Add GUI desktop support fields to projects table
ALTER TABLE kubetty_projects ADD COLUMN IF NOT EXISTS gui_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE kubetty_projects ADD COLUMN IF NOT EXISTS gui_resolution TEXT DEFAULT '1920x1080x24';
ALTER TABLE kubetty_projects ADD COLUMN IF NOT EXISTS gui_vnc_port INTEGER DEFAULT 5901;

-- Add comment explaining the fields
COMMENT ON COLUMN kubetty_projects.gui_enabled IS 'Enable GUI desktop support (noVNC + XFCE)';
COMMENT ON COLUMN kubetty_projects.gui_resolution IS 'X display resolution (e.g., 1920x1080x24)';
COMMENT ON COLUMN kubetty_projects.gui_vnc_port IS 'VNC server port for GUI access';
