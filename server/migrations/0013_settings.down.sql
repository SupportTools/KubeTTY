-- Drop trigger first
DROP TRIGGER IF EXISTS settings_history_trigger ON kubetty_settings;

-- Drop trigger function
DROP FUNCTION IF EXISTS record_settings_history();

-- Drop indexes
DROP INDEX IF EXISTS idx_settings_history_changed_at;
DROP INDEX IF EXISTS idx_settings_history_category_key;
DROP INDEX IF EXISTS idx_settings_history_setting;
DROP INDEX IF EXISTS idx_settings_key;
DROP INDEX IF EXISTS idx_settings_category;

-- Drop tables (history first due to FK)
DROP TABLE IF EXISTS kubetty_settings_history;
DROP TABLE IF EXISTS kubetty_settings;
