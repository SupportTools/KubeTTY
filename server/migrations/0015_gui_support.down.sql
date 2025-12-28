-- Remove GUI desktop support fields from projects table
ALTER TABLE kubetty_projects DROP COLUMN IF EXISTS gui_vnc_port;
ALTER TABLE kubetty_projects DROP COLUMN IF EXISTS gui_resolution;
ALTER TABLE kubetty_projects DROP COLUMN IF EXISTS gui_enabled;
