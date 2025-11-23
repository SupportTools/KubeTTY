package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors returned by the project store.
var (
	ErrProjectNotFound      = errors.New("project not found")
	ErrDuplicateName        = errors.New("project name already exists")
	ErrDuplicateNamespace   = errors.New("target namespace already exists")
	ErrDuplicateServiceName = errors.New("service name already exists")
	ErrInvalidName          = errors.New("invalid project name: must be lowercase alphanumeric with dashes")
)

// DNS-1123 subdomain name pattern for Kubernetes resources
var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Store exposes project persistence operations.
type Store interface {
	Create(ctx context.Context, req CreateProjectRequest) (*Project, error)
	Get(ctx context.Context, id uuid.UUID) (*Project, error)
	GetByName(ctx context.Context, name string) (*Project, error)
	GetByServiceName(ctx context.Context, serviceName string) (*Project, error)
	List(ctx context.Context, filter ListFilter) ([]Project, error)
	Update(ctx context.Context, id uuid.UUID, req UpdateProjectRequest) (*Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
	HardDelete(ctx context.Context, id uuid.UUID) error

	// Status updates
	SetStatus(ctx context.Context, id uuid.UUID, status ProjectStatus, message string) error
	UpdateHealthCheck(ctx context.Context, id uuid.UUID, podIP string) error

	// List projects by status for controller reconciliation
	ListByStatuses(ctx context.Context, statuses []ProjectStatus) ([]Project, error)
}

// PGStore is a pgx-backed Store implementation.
type PGStore struct {
	pool *pgxpool.Pool
}

// NewStore creates a new store using its own connection pool.
func NewStore(ctx context.Context, config *pgxpool.Config) (*PGStore, error) {
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("project store connect: %w", err)
	}
	return &PGStore{pool: pool}, nil
}

// Close releases the connection pool.
func (s *PGStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// NewStoreFromPool reuses an existing pool.
func NewStoreFromPool(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) Create(ctx context.Context, req CreateProjectRequest) (*Project, error) {
	// Validate name
	if !namePattern.MatchString(req.Name) {
		return nil, ErrInvalidName
	}

	// Apply defaults
	req.ApplyDefaults()

	// Generate IDs and derived values
	id := uuid.New()
	sessionID := uuid.New()
	targetNamespace := fmt.Sprintf("kubetty-%s", req.Name)
	serviceName := ComputeServiceName(req.Name)

	// Serialize JSON fields
	adminNS, _ := json.Marshal(req.AdminNamespaces)
	readNS, _ := json.Marshal(req.ReadNamespaces)
	envVars, _ := json.Marshal(req.EnvVars)

	const stmt = `
INSERT INTO kubetty_projects (
    id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status
)
VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13,
    $14, $15,
    $16, $17,
    $18, $19,
    $20, $21,
    $22, $23,
    'pending'
)
RETURNING id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status, status_message, last_health_check, pod_ip,
    created_at, updated_at, deleted_at`

	row := s.pool.QueryRow(ctx, stmt,
		id, req.Name, req.DisplayName, nullIfEmpty(req.Description), nullIfEmpty(req.Icon),
		targetNamespace, serviceName, sessionID, req.UserName,
		req.CPURequest, req.CPULimit, req.MemoryRequest, req.MemoryLimit,
		req.StorageSize, req.StorageClass,
		adminNS, readNS,
		req.MaxTabsPerClient, req.MaxTabsTotal,
		*req.DinDEnabled, envVars,
		req.ImageRepository, req.ImageTag,
	)

	return scanProject(row)
}

func (s *PGStore) Get(ctx context.Context, id uuid.UUID) (*Project, error) {
	const stmt = `
SELECT id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status, status_message, last_health_check, pod_ip,
    created_at, updated_at, deleted_at
FROM kubetty_projects
WHERE id = $1 AND deleted_at IS NULL`

	row := s.pool.QueryRow(ctx, stmt, id)
	return scanProject(row)
}

func (s *PGStore) GetByName(ctx context.Context, name string) (*Project, error) {
	const stmt = `
SELECT id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status, status_message, last_health_check, pod_ip,
    created_at, updated_at, deleted_at
FROM kubetty_projects
WHERE name = $1 AND deleted_at IS NULL`

	row := s.pool.QueryRow(ctx, stmt, name)
	return scanProject(row)
}

func (s *PGStore) GetByServiceName(ctx context.Context, serviceName string) (*Project, error) {
	const stmt = `
SELECT id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status, status_message, last_health_check, pod_ip,
    created_at, updated_at, deleted_at
FROM kubetty_projects
WHERE service_name = $1 AND deleted_at IS NULL`

	row := s.pool.QueryRow(ctx, stmt, serviceName)
	return scanProject(row)
}

func (s *PGStore) List(ctx context.Context, filter ListFilter) ([]Project, error) {
	query := `
SELECT id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status, status_message, last_health_check, pod_ip,
    created_at, updated_at, deleted_at
FROM kubetty_projects
WHERE 1=1`

	args := []interface{}{}
	argN := 1

	if !filter.IncludeAll {
		query += " AND deleted_at IS NULL"
	}

	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argN)
		args = append(args, filter.Status)
		argN++
	}

	if filter.UserName != "" {
		query += fmt.Sprintf(" AND user_name = $%d", argN)
		args = append(args, filter.UserName)
		argN++
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argN)
		args = append(args, filter.Limit)
		argN++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argN)
		args = append(args, filter.Offset)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		project, err := scanProjectRows(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, *project)
	}
	return projects, rows.Err()
}

func (s *PGStore) ListByStatuses(ctx context.Context, statuses []ProjectStatus) ([]Project, error) {
	if len(statuses) == 0 {
		return []Project{}, nil
	}

	query := `
SELECT id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status, status_message, last_health_check, pod_ip,
    created_at, updated_at, deleted_at
FROM kubetty_projects
WHERE deleted_at IS NULL AND status = ANY($1)
ORDER BY created_at ASC`

	// Convert to string slice for pgx
	statusStrings := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrings[i] = string(s)
	}

	rows, err := s.pool.Query(ctx, query, statusStrings)
	if err != nil {
		return nil, fmt.Errorf("list projects by status: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		project, err := scanProjectRows(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, *project)
	}
	return projects, rows.Err()
}

func (s *PGStore) Update(ctx context.Context, id uuid.UUID, req UpdateProjectRequest) (*Project, error) {
	// Build dynamic UPDATE query
	setClauses := []string{}
	args := []interface{}{id}
	argN := 2

	if req.DisplayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argN))
		args = append(args, *req.DisplayName)
		argN++
	}
	if req.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argN))
		args = append(args, nullIfEmpty(*req.Description))
		argN++
	}
	if req.Icon != nil {
		setClauses = append(setClauses, fmt.Sprintf("icon = $%d", argN))
		args = append(args, nullIfEmpty(*req.Icon))
		argN++
	}
	if req.CPURequest != nil {
		setClauses = append(setClauses, fmt.Sprintf("cpu_request = $%d", argN))
		args = append(args, *req.CPURequest)
		argN++
	}
	if req.CPULimit != nil {
		setClauses = append(setClauses, fmt.Sprintf("cpu_limit = $%d", argN))
		args = append(args, *req.CPULimit)
		argN++
	}
	if req.MemoryRequest != nil {
		setClauses = append(setClauses, fmt.Sprintf("memory_request = $%d", argN))
		args = append(args, *req.MemoryRequest)
		argN++
	}
	if req.MemoryLimit != nil {
		setClauses = append(setClauses, fmt.Sprintf("memory_limit = $%d", argN))
		args = append(args, *req.MemoryLimit)
		argN++
	}
	if req.MaxTabsPerClient != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_tabs_per_client = $%d", argN))
		args = append(args, *req.MaxTabsPerClient)
		argN++
	}
	if req.MaxTabsTotal != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_tabs_total = $%d", argN))
		args = append(args, *req.MaxTabsTotal)
		argN++
	}
	if req.DinDEnabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("dind_enabled = $%d", argN))
		args = append(args, *req.DinDEnabled)
		argN++
	}
	if req.EnvVars != nil {
		envJSON, _ := json.Marshal(req.EnvVars)
		setClauses = append(setClauses, fmt.Sprintf("env_vars = $%d", argN))
		args = append(args, envJSON)
		argN++
	}
	if req.ImageTag != nil {
		setClauses = append(setClauses, fmt.Sprintf("image_tag = $%d", argN))
		args = append(args, *req.ImageTag)
		argN++
	}

	if len(setClauses) == 0 {
		// No updates, just return current state
		return s.Get(ctx, id)
	}

	query := fmt.Sprintf(`
UPDATE kubetty_projects
SET %s
WHERE id = $1 AND deleted_at IS NULL
RETURNING id, name, display_name, description, icon,
    target_namespace, service_name, session_id, user_name,
    cpu_request, cpu_limit, memory_request, memory_limit,
    storage_size, storage_class,
    admin_namespaces, read_namespaces,
    max_tabs_per_client, max_tabs_total,
    dind_enabled, env_vars,
    image_repository, image_tag,
    status, status_message, last_health_check, pod_ip,
    created_at, updated_at, deleted_at`,
		joinStrings(setClauses, ", "))

	row := s.pool.QueryRow(ctx, query, args...)
	return scanProject(row)
}

func (s *PGStore) Delete(ctx context.Context, id uuid.UUID) error {
	const stmt = `
UPDATE kubetty_projects
SET deleted_at = NOW(), status = 'deleting'
WHERE id = $1 AND deleted_at IS NULL`

	tag, err := s.pool.Exec(ctx, stmt, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (s *PGStore) HardDelete(ctx context.Context, id uuid.UUID) error {
	const stmt = `DELETE FROM kubetty_projects WHERE id = $1`

	tag, err := s.pool.Exec(ctx, stmt, id)
	if err != nil {
		return fmt.Errorf("hard delete project: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (s *PGStore) SetStatus(ctx context.Context, id uuid.UUID, status ProjectStatus, message string) error {
	const stmt = `
UPDATE kubetty_projects
SET status = $2, status_message = $3
WHERE id = $1`

	tag, err := s.pool.Exec(ctx, stmt, id, status, nullIfEmpty(message))
	if err != nil {
		return fmt.Errorf("set project status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (s *PGStore) UpdateHealthCheck(ctx context.Context, id uuid.UUID, podIP string) error {
	const stmt = `
UPDATE kubetty_projects
SET last_health_check = NOW(), pod_ip = $2
WHERE id = $1 AND deleted_at IS NULL`

	tag, err := s.pool.Exec(ctx, stmt, id, nullIfEmpty(podIP))
	if err != nil {
		return fmt.Errorf("update health check: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func scanProject(row pgx.Row) (*Project, error) {
	var p Project
	var description, icon, statusMessage, podIP, serviceName *string
	var adminNS, readNS, envVars []byte

	err := row.Scan(
		&p.ID, &p.Name, &p.DisplayName, &description, &icon,
		&p.TargetNamespace, &serviceName, &p.SessionID, &p.UserName,
		&p.CPURequest, &p.CPULimit, &p.MemoryRequest, &p.MemoryLimit,
		&p.StorageSize, &p.StorageClass,
		&adminNS, &readNS,
		&p.MaxTabsPerClient, &p.MaxTabsTotal,
		&p.DinDEnabled, &envVars,
		&p.ImageRepository, &p.ImageTag,
		&p.Status, &statusMessage, &p.LastHealthCheck, &podIP,
		&p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProjectNotFound
		}
		return nil, fmt.Errorf("scan project: %w", err)
	}

	// Handle nullable strings
	if description != nil {
		p.Description = *description
	}
	if icon != nil {
		p.Icon = *icon
	}
	if serviceName != nil {
		p.ServiceName = *serviceName
	}
	if statusMessage != nil {
		p.StatusMessage = *statusMessage
	}
	if podIP != nil {
		p.PodIP = *podIP
	}

	// Parse JSON fields
	if err := json.Unmarshal(adminNS, &p.AdminNamespaces); err != nil {
		p.AdminNamespaces = []string{}
	}
	if err := json.Unmarshal(readNS, &p.ReadNamespaces); err != nil {
		p.ReadNamespaces = []string{}
	}
	if err := json.Unmarshal(envVars, &p.EnvVars); err != nil {
		p.EnvVars = map[string]string{}
	}

	return &p, nil
}

func scanProjectRows(rows pgx.Rows) (*Project, error) {
	var p Project
	var description, icon, statusMessage, podIP, serviceName *string
	var adminNS, readNS, envVars []byte

	err := rows.Scan(
		&p.ID, &p.Name, &p.DisplayName, &description, &icon,
		&p.TargetNamespace, &serviceName, &p.SessionID, &p.UserName,
		&p.CPURequest, &p.CPULimit, &p.MemoryRequest, &p.MemoryLimit,
		&p.StorageSize, &p.StorageClass,
		&adminNS, &readNS,
		&p.MaxTabsPerClient, &p.MaxTabsTotal,
		&p.DinDEnabled, &envVars,
		&p.ImageRepository, &p.ImageTag,
		&p.Status, &statusMessage, &p.LastHealthCheck, &podIP,
		&p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan project row: %w", err)
	}

	// Handle nullable strings
	if description != nil {
		p.Description = *description
	}
	if icon != nil {
		p.Icon = *icon
	}
	if serviceName != nil {
		p.ServiceName = *serviceName
	}
	if statusMessage != nil {
		p.StatusMessage = *statusMessage
	}
	if podIP != nil {
		p.PodIP = *podIP
	}

	// Parse JSON fields
	if err := json.Unmarshal(adminNS, &p.AdminNamespaces); err != nil {
		p.AdminNamespaces = []string{}
	}
	if err := json.Unmarshal(readNS, &p.ReadNamespaces); err != nil {
		p.ReadNamespaces = []string{}
	}
	if err := json.Unmarshal(envVars, &p.EnvVars); err != nil {
		p.EnvVars = map[string]string{}
	}

	return &p, nil
}

func nullIfEmpty(val string) interface{} {
	if val == "" {
		return nil
	}
	return val
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
