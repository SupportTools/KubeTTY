# Admin Dashboard Design Plan

## Overview

Create a comprehensive Admin Dashboard for KubeTTY that provides visibility into system health, connection metrics, failures, and usage patterns. The dashboard will be accessible from the gateway UI for admin users.

## Data Sources Available

### 1. Prometheus Metrics (real-time)
- **WebSocket Connections**:
  - `kubetty_websocket_connections_total` - Total connection attempts
  - `kubetty_websocket_connections_active` - Currently active connections
  - `kubetty_websocket_disconnects_total{reason}` - Disconnections by reason (normal, error, timeout, pty_exit, pty_write_error)
  - `kubetty_websocket_write_errors_total` - Write failures
  - `kubetty_websocket_flow_control_pauses_total` - Flow control events
- **Session Metrics**:
  - `kubetty_session_attached_total` - Sessions attached
  - `kubetty_session_detached_total` - Sessions detached
  - `kubetty_pty_exit_total{exit_code}` - PTY exits by code
- **HTTP Metrics**:
  - `kubetty_http_requests_total{method,path,status}` - Request counts
  - `kubetty_http_request_duration_seconds` - Request latencies
- **Store Metrics**:
  - `kubetty_store_duration_seconds{operation}` - DB operation latencies
  - `kubetty_store_errors_total{operation}` - DB errors

### 2. Database Tables (historical)
- `kubetty_projects` - Project status, health, activity
- `gateway_tabs` - Tab lifecycle, errors, status history
- `sessions` - Session metadata
- `session_logs` - Activity logs (searchable)

## Dashboard Components

### 1. Overview Summary Panel
**Purpose**: Quick system health at-a-glance

**Displays**:
- Active WebSocket connections (gauge)
- Total projects (running/failed/total)
- Active tabs count
- Last 24h stats:
  - Total connections
  - Total disconnects
  - Error rate percentage

### 2. Connection Metrics Panel
**Purpose**: WebSocket connection health and patterns

**Displays**:
- Active connections over time (line chart, 1h window)
- Disconnects by reason (pie/donut chart)
- Connection rate (connections/min)
- Error rate (errors/total %)
- Flow control pauses count

### 3. Project Status Panel
**Purpose**: Project health overview

**Displays**:
- Project count by status (running, failed, creating, etc.)
- Projects with recent errors
- Projects with no recent activity
- Last health check times

### 4. Tab Activity Panel
**Purpose**: Tab lifecycle monitoring

**Displays**:
- Active tabs by project
- Tab status distribution
- Recent tab errors (last 10)
- Tab creation rate

### 5. Error Log Panel
**Purpose**: Recent errors for troubleshooting

**Displays**:
- Recent WebSocket disconnects (with reasons)
- Recent project status changes to 'failed'
- Recent tab errors
- Filterable by project/time range

### 6. Usage Analytics Panel
**Purpose**: Usage patterns and trends

**Displays**:
- Connections by hour (bar chart)
- Top projects by connection count
- Average session duration
- Peak usage times

## API Endpoints

### New Endpoints for Dashboard

```
GET /api/admin/dashboard/summary
Response:
{
  "activeConnections": 42,
  "projects": {
    "running": 15,
    "failed": 2,
    "total": 20
  },
  "tabs": {
    "active": 35,
    "total": 50
  },
  "last24h": {
    "connections": 1250,
    "disconnects": 1200,
    "errors": 15,
    "errorRate": 1.2
  }
}

GET /api/admin/dashboard/metrics
Query params: ?period=1h|24h|7d
Response:
{
  "period": "1h",
  "connectionTimeseries": [
    {"timestamp": "...", "active": 42, "connects": 5, "disconnects": 3}
  ],
  "disconnectsByReason": {
    "normal": 100,
    "error": 5,
    "timeout": 10,
    "pty_exit": 85
  },
  "flowControlPauses": 3,
  "writeErrors": 1
}

GET /api/admin/dashboard/errors
Query params: ?limit=50&project=<id>
Response:
{
  "errors": [
    {
      "type": "disconnect",
      "reason": "error",
      "projectId": "...",
      "projectName": "...",
      "timestamp": "...",
      "details": "..."
    }
  ]
}

GET /api/admin/dashboard/usage
Query params: ?period=24h|7d|30d
Response:
{
  "period": "24h",
  "hourlyConnections": [
    {"hour": "2024-01-15T00:00:00Z", "count": 50}
  ],
  "topProjects": [
    {"projectId": "...", "name": "...", "connections": 500}
  ],
  "peakHour": "14:00",
  "avgSessionDuration": 3600
}
```

## Implementation Plan

### Phase 1: Backend API (Server)

#### Task 1.1: Create Dashboard Handler Package
- Location: `server/internal/handlers/dashboard/`
- Files:
  - `handlers.go` - Main handler struct and constructor
  - `summary.go` - Summary endpoint
  - `metrics.go` - Metrics endpoint
  - `errors.go` - Errors endpoint
  - `usage.go` - Usage endpoint

#### Task 1.2: Create Metrics Query Service
- Location: `server/internal/dashboard/`
- Files:
  - `metrics.go` - Query Prometheus metrics
  - `aggregator.go` - Aggregate data from multiple sources
- Implements:
  - Query current Prometheus counter/gauge values
  - Query historical data from Prometheus (if available) or DB

#### Task 1.3: Database Queries for Dashboard
- Add methods to existing stores:
  - `projects.Store.GetStatusCounts()` - Count by status
  - `projects.Store.GetRecentlyFailed()` - Failed projects
  - `tabs.Store.GetActiveCountsByProject()` - Tab counts
  - `tabs.Store.GetRecentErrors()` - Recent tab errors

#### Task 1.4: Register Dashboard Routes
- Add routes to gateway main.go under `/api/admin/dashboard/*`
- Protected by admin auth middleware

### Phase 2: Frontend UI (Web)

#### Task 2.1: Create Dashboard Component Structure
- Location: `web/src/components/dashboard/`
- Files:
  - `AdminDashboard.tsx` - Main dashboard container
  - `SummaryPanel.tsx` - Overview cards
  - `ConnectionMetrics.tsx` - Connection charts
  - `ProjectStatus.tsx` - Project health grid
  - `ErrorLog.tsx` - Error table
  - `UsageAnalytics.tsx` - Usage charts

#### Task 2.2: Add Dashboard Types
- Update `web/src/types.ts` with:
  - `DashboardSummary`
  - `DashboardMetrics`
  - `DashboardError`
  - `DashboardUsage`

#### Task 2.3: Create Dashboard Entry Point
- Add "Dashboard" button to admin toolbar in gateway UI
- Create modal or dedicated view for dashboard

#### Task 2.4: Implement Charts
- Use lightweight chart library (recharts or chart.js)
- Implement:
  - Line chart for connections over time
  - Pie/donut chart for disconnect reasons
  - Bar chart for hourly usage

#### Task 2.5: Implement Auto-Refresh
- 30-second refresh interval for metrics
- SSE or polling for real-time updates

### Phase 3: CSS Styling

#### Task 3.1: Dashboard Styles
- Grid layout for panels
- Responsive design
- Status colors consistent with existing UI
- Card-based panel design

## File Changes Summary

### New Files
```
server/internal/handlers/dashboard/
  ├── handlers.go
  ├── summary.go
  ├── metrics.go
  ├── errors.go
  └── usage.go

server/internal/dashboard/
  ├── metrics.go
  └── aggregator.go

web/src/components/dashboard/
  ├── AdminDashboard.tsx
  ├── SummaryPanel.tsx
  ├── ConnectionMetrics.tsx
  ├── ProjectStatus.tsx
  ├── ErrorLog.tsx
  └── UsageAnalytics.tsx
```

### Modified Files
```
server/cmd/gateway/main.go         - Add dashboard routes
server/internal/projects/store.go  - Add dashboard query methods
web/src/types.ts                   - Add dashboard types
web/src/App.tsx                    - Add dashboard entry point
web/src/index.css                  - Add dashboard styles
```

## Dependencies

### Backend
- No new dependencies (use existing prometheus client, pgx)

### Frontend
- Add: `recharts` (lightweight React charts) or use canvas-based charts

## Testing Plan

### Backend Tests
- Unit tests for each handler
- Integration tests with test database
- Mock Prometheus metrics for testing

### Frontend Tests
- Component rendering tests
- API response handling tests
- Error state handling

## Security Considerations

- All dashboard endpoints require admin authentication
- Rate limiting on dashboard queries
- No sensitive data exposed (passwords, tokens, etc.)
- Sanitize any user-provided filter inputs

## Estimated Effort

| Phase | Tasks | Complexity |
|-------|-------|------------|
| Phase 1 | Backend API | Medium |
| Phase 2 | Frontend UI | Medium-High |
| Phase 3 | Styling | Low |

## Open Questions

1. Should dashboard data be cached to reduce DB load?
2. Do we need historical data persistence beyond Prometheus retention?
3. Should we add alerting thresholds (e.g., notify when error rate > X%)?

## Next Steps

1. User approves this plan
2. Create TaskForge feature and tasks
3. Implement Phase 1 (Backend)
4. Implement Phase 2 (Frontend)
5. Implement Phase 3 (Styling)
6. Integration testing
7. Deploy to dev for validation
