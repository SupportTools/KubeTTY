-- Add last_activity tracking for project upgrade safety
ALTER TABLE kubetty_projects
ADD COLUMN last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- Update existing projects to have current timestamp
UPDATE kubetty_projects
SET last_activity = CURRENT_TIMESTAMP
WHERE last_activity IS NULL;
