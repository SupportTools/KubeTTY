package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool defines the interface for database pool operations.
type Pool interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Close()
}

// Errors returned by the settings store.
var (
	ErrSettingNotFound  = errors.New("setting not found")
	ErrDuplicateSetting = errors.New("setting already exists")
	ErrInvalidCategory  = errors.New("invalid category")
	ErrInvalidValueType = errors.New("invalid value type")
	ErrSettingReadonly  = errors.New("setting is read-only")
)

// Store exposes settings persistence operations.
type Store interface {
	// CRUD operations
	Get(ctx context.Context, category SettingCategory, key string) (*Setting, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Setting, error)
	List(ctx context.Context, category SettingCategory) ([]Setting, error)
	ListAll(ctx context.Context) ([]Setting, error)
	Create(ctx context.Context, req CreateSettingRequest, changedBy string) (*Setting, error)
	Update(ctx context.Context, category SettingCategory, key string, value interface{}, changedBy string) (*Setting, error)
	Delete(ctx context.Context, category SettingCategory, key string, changedBy string) error

	// History operations
	GetHistory(ctx context.Context, category SettingCategory, key string, filter HistoryFilter) ([]SettingHistory, error)
	GetAllHistory(ctx context.Context, filter HistoryFilter) ([]SettingHistory, error)

	// Type-safe getters with defaults (for integration with other packages)
	GetString(ctx context.Context, category SettingCategory, key string, defaultVal string) string
	GetInt(ctx context.Context, category SettingCategory, key string, defaultVal int) int
	GetBool(ctx context.Context, category SettingCategory, key string, defaultVal bool) bool
	GetDuration(ctx context.Context, category SettingCategory, key string, defaultVal string) string
}

// PGStore is a pgx-backed Store implementation.
type PGStore struct {
	pool Pool
}

// NewStore creates a new store using its own connection pool.
func NewStore(ctx context.Context, config *pgxpool.Config) (*PGStore, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("settings store connect: %w", err)
	}
	return &PGStore{pool: pool}, nil
}

// NewStoreFromPool reuses an existing pool.
func NewStoreFromPool(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

// NewStoreWithPool creates a PGStore with a pre-configured pool.
// This is primarily used for testing with mock pools.
func NewStoreWithPool(pool Pool) *PGStore {
	return &PGStore{pool: pool}
}

// Close releases the connection pool.
func (s *PGStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *PGStore) Get(ctx context.Context, category SettingCategory, key string) (*Setting, error) {
	const stmt = `
		SELECT id, category, key, value, value_type, display_name, description,
		       is_sensitive, is_readonly, validation, created_at, updated_at
		FROM kubetty_settings
		WHERE category = $1 AND key = $2
	`

	row := s.pool.QueryRow(ctx, stmt, string(category), key)
	return scanSetting(row)
}

func (s *PGStore) GetByID(ctx context.Context, id uuid.UUID) (*Setting, error) {
	const stmt = `
		SELECT id, category, key, value, value_type, display_name, description,
		       is_sensitive, is_readonly, validation, created_at, updated_at
		FROM kubetty_settings
		WHERE id = $1
	`

	row := s.pool.QueryRow(ctx, stmt, id)
	return scanSetting(row)
}

func (s *PGStore) List(ctx context.Context, category SettingCategory) ([]Setting, error) {
	const stmt = `
		SELECT id, category, key, value, value_type, display_name, description,
		       is_sensitive, is_readonly, validation, created_at, updated_at
		FROM kubetty_settings
		WHERE category = $1
		ORDER BY key
	`

	rows, err := s.pool.Query(ctx, stmt, string(category))
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	return scanSettings(rows)
}

func (s *PGStore) ListAll(ctx context.Context) ([]Setting, error) {
	const stmt = `
		SELECT id, category, key, value, value_type, display_name, description,
		       is_sensitive, is_readonly, validation, created_at, updated_at
		FROM kubetty_settings
		ORDER BY category, key
	`

	rows, err := s.pool.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("list all settings: %w", err)
	}
	defer rows.Close()

	return scanSettings(rows)
}

func (s *PGStore) Create(ctx context.Context, req CreateSettingRequest, changedBy string) (*Setting, error) {
	if !req.Category.IsValid() {
		return nil, ErrInvalidCategory
	}

	// Set audit context for trigger
	if err := s.setAuditContext(ctx, changedBy, "api"); err != nil {
		return nil, fmt.Errorf("set audit context: %w", err)
	}

	valueJSON, err := json.Marshal(req.Value)
	if err != nil {
		return nil, fmt.Errorf("marshal value: %w", err)
	}

	var validationJSON []byte
	if req.Validation != nil {
		validationJSON, err = json.Marshal(req.Validation)
		if err != nil {
			return nil, fmt.Errorf("marshal validation: %w", err)
		}
	}

	id := uuid.New()

	const stmt = `
		INSERT INTO kubetty_settings (
			id, category, key, value, value_type, display_name, description,
			is_sensitive, is_readonly, validation
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, category, key, value, value_type, display_name, description,
		          is_sensitive, is_readonly, validation, created_at, updated_at
	`

	row := s.pool.QueryRow(ctx, stmt,
		id,
		string(req.Category),
		req.Key,
		valueJSON,
		string(req.ValueType),
		req.DisplayName,
		nullIfEmpty(req.Description),
		req.IsSensitive,
		req.IsReadonly,
		validationJSON,
	)

	return scanSetting(row)
}

func (s *PGStore) Update(ctx context.Context, category SettingCategory, key string, value interface{}, changedBy string) (*Setting, error) {
	// First check if setting exists and is not readonly
	existing, err := s.Get(ctx, category, key)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return nil, ErrSettingNotFound
		}
		return nil, fmt.Errorf("get setting: %w", err)
	}

	if existing.IsReadonly {
		return nil, ErrSettingReadonly
	}

	// Set audit context for trigger
	if err := s.setAuditContext(ctx, changedBy, "api"); err != nil {
		return nil, fmt.Errorf("set audit context: %w", err)
	}

	valueJSON, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal value: %w", err)
	}

	const stmt = `
		UPDATE kubetty_settings
		SET value = $1
		WHERE category = $2 AND key = $3
		RETURNING id, category, key, value, value_type, display_name, description,
		          is_sensitive, is_readonly, validation, created_at, updated_at
	`

	row := s.pool.QueryRow(ctx, stmt, valueJSON, string(category), key)
	return scanSetting(row)
}

func (s *PGStore) Delete(ctx context.Context, category SettingCategory, key string, changedBy string) error {
	// Check if setting exists and is not readonly
	existing, err := s.Get(ctx, category, key)
	if err != nil {
		return err
	}

	if existing.IsReadonly {
		return ErrSettingReadonly
	}

	// Set audit context for trigger
	if err := s.setAuditContext(ctx, changedBy, "api"); err != nil {
		return fmt.Errorf("set audit context: %w", err)
	}

	const stmt = `DELETE FROM kubetty_settings WHERE category = $1 AND key = $2`
	tag, err := s.pool.Exec(ctx, stmt, string(category), key)
	if err != nil {
		return fmt.Errorf("delete setting: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrSettingNotFound
	}

	return nil
}

func (s *PGStore) GetHistory(ctx context.Context, category SettingCategory, key string, filter HistoryFilter) ([]SettingHistory, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	const stmt = `
		SELECT id, setting_id, category, key, old_value, new_value, change_type,
		       changed_by, changed_at, change_source, change_reason, client_ip, user_agent
		FROM kubetty_settings_history
		WHERE category = $1 AND key = $2
		ORDER BY changed_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := s.pool.Query(ctx, stmt, string(category), key, limit, filter.Offset)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	defer rows.Close()

	return scanHistoryRows(rows)
}

func (s *PGStore) GetAllHistory(ctx context.Context, filter HistoryFilter) ([]SettingHistory, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	const stmt = `
		SELECT id, setting_id, category, key, old_value, new_value, change_type,
		       changed_by, changed_at, change_source, change_reason, client_ip, user_agent
		FROM kubetty_settings_history
		ORDER BY changed_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.pool.Query(ctx, stmt, limit, filter.Offset)
	if err != nil {
		return nil, fmt.Errorf("get all history: %w", err)
	}
	defer rows.Close()

	return scanHistoryRows(rows)
}

// GetString returns the setting value as a string, or defaultVal if not found.
func (s *PGStore) GetString(ctx context.Context, category SettingCategory, key string, defaultVal string) string {
	setting, err := s.Get(ctx, category, key)
	if err != nil {
		return defaultVal
	}
	return setting.GetString(defaultVal)
}

// GetInt returns the setting value as an int, or defaultVal if not found.
func (s *PGStore) GetInt(ctx context.Context, category SettingCategory, key string, defaultVal int) int {
	setting, err := s.Get(ctx, category, key)
	if err != nil {
		return defaultVal
	}
	return setting.GetInt(defaultVal)
}

// GetBool returns the setting value as a bool, or defaultVal if not found.
func (s *PGStore) GetBool(ctx context.Context, category SettingCategory, key string, defaultVal bool) bool {
	setting, err := s.Get(ctx, category, key)
	if err != nil {
		return defaultVal
	}
	return setting.GetBool(defaultVal)
}

// GetDuration returns the setting value as a duration string, or defaultVal if not found.
func (s *PGStore) GetDuration(ctx context.Context, category SettingCategory, key string, defaultVal string) string {
	return s.GetString(ctx, category, key, defaultVal)
}

// setAuditContext sets PostgreSQL session variables for the history trigger.
func (s *PGStore) setAuditContext(ctx context.Context, changedBy, changeSource string) error {
	_, err := s.pool.Exec(ctx, "SELECT set_config('kubetty.changed_by', $1, true)", changedBy)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, "SELECT set_config('kubetty.change_source', $1, true)", changeSource)
	return err
}

// Helper functions

func scanSetting(row pgx.Row) (*Setting, error) {
	var setting Setting
	var description, validation *string

	err := row.Scan(
		&setting.ID,
		&setting.Category,
		&setting.Key,
		&setting.Value,
		&setting.ValueType,
		&setting.DisplayName,
		&description,
		&setting.IsSensitive,
		&setting.IsReadonly,
		&validation,
		&setting.CreatedAt,
		&setting.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettingNotFound
		}
		return nil, fmt.Errorf("scan setting: %w", err)
	}

	if description != nil {
		setting.Description = *description
	}
	if validation != nil {
		setting.Validation = json.RawMessage(*validation)
	}

	return &setting, nil
}

func scanSettings(rows pgx.Rows) ([]Setting, error) {
	var settings []Setting
	for rows.Next() {
		var setting Setting
		var description, validation *string

		err := rows.Scan(
			&setting.ID,
			&setting.Category,
			&setting.Key,
			&setting.Value,
			&setting.ValueType,
			&setting.DisplayName,
			&description,
			&setting.IsSensitive,
			&setting.IsReadonly,
			&validation,
			&setting.CreatedAt,
			&setting.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan setting row: %w", err)
		}

		if description != nil {
			setting.Description = *description
		}
		if validation != nil {
			setting.Validation = json.RawMessage(*validation)
		}

		settings = append(settings, setting)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return settings, nil
}

func scanHistoryRows(rows pgx.Rows) ([]SettingHistory, error) {
	var history []SettingHistory
	for rows.Next() {
		var h SettingHistory
		var settingID *uuid.UUID
		var oldValue, newValue []byte
		var changedBy, changeReason, clientIP, userAgent *string

		err := rows.Scan(
			&h.ID,
			&settingID,
			&h.Category,
			&h.Key,
			&oldValue,
			&newValue,
			&h.ChangeType,
			&changedBy,
			&h.ChangedAt,
			&h.ChangeSource,
			&changeReason,
			&clientIP,
			&userAgent,
		)
		if err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}

		h.SettingID = settingID
		if oldValue != nil {
			h.OldValue = oldValue
		}
		if newValue != nil {
			h.NewValue = newValue
		}
		if changedBy != nil {
			h.ChangedBy = *changedBy
		}
		if changeReason != nil {
			h.ChangeReason = *changeReason
		}
		if clientIP != nil {
			h.ClientIP = *clientIP
		}
		if userAgent != nil {
			h.UserAgent = *userAgent
		}

		history = append(history, h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return history, nil
}

func nullIfEmpty(val string) interface{} {
	if val == "" {
		return nil
	}
	return val
}
