# GUI Desktop Support - Implementation Plan

This document outlines the phased implementation plan for adding GUI desktop support (noVNC + XFCE) to KubeTTY, as specified in DESIGN.md Section 14.

## Overview

**Goal:** Enable users to run GUI applications (Chrome, Firefox, VS Code) inside project pods with browser-based access via a split-view UI.

**Key Features:**
- XFCE desktop environment
- noVNC streaming via WebSocket
- Split-view UI (Terminal + GUI side-by-side)
- Resizable panes
- Keyboard shortcuts

---

## Phase 1: Database & Data Model

**Duration:** ~2-3 hours

### Tasks

1. **Create database migration** (`server/migrations/0015_gui_support.up.sql`)
   ```sql
   ALTER TABLE projects ADD COLUMN gui_enabled BOOLEAN NOT NULL DEFAULT false;
   ALTER TABLE projects ADD COLUMN gui_resolution TEXT DEFAULT '1920x1080x24';
   ALTER TABLE projects ADD COLUMN gui_vnc_port INTEGER DEFAULT 5901;
   ```

2. **Create rollback migration** (`server/migrations/0015_gui_support.down.sql`)
   ```sql
   ALTER TABLE projects DROP COLUMN IF EXISTS gui_vnc_port;
   ALTER TABLE projects DROP COLUMN IF EXISTS gui_resolution;
   ALTER TABLE projects DROP COLUMN IF EXISTS gui_enabled;
   ```

3. **Update Go models** (`server/internal/projects/models.go`)
   - Add `GUIEnabled`, `GUIResolution`, `GUIVNCPort` fields
   - Update JSON tags

4. **Update project store** (`server/internal/projects/store.go`)
   - Add GUI fields to INSERT/UPDATE/SELECT queries
   - Update `scanProject` function

5. **Update TypeScript types** (`web/src/types.ts`)
   - Add `guiEnabled`, `guiResolution` to `ProjectInfo` and `AdminProject`
   - Add `ViewMode` type

### Deliverables
- [ ] Migration files created and tested
- [ ] Go models updated with GUI fields
- [ ] TypeScript types updated
- [ ] Existing tests pass

---

## Phase 2: Gateway VNC Relay

**Duration:** ~4-6 hours

### Tasks

1. **Create VNC relay package** (`server/internal/gateway/vnc/`)

   **relay.go:**
   ```go
   type VNCRelay struct {
       vncConn    net.Conn
       target     string
       statusCh   chan relay.StatusEvent
       activityCh chan struct{}
       mu         sync.Mutex
       closed     bool
   }

   func NewRelay(target string) (*VNCRelay, error)
   func (r *VNCRelay) Proxy(ctx context.Context, upstream *websocket.Conn) error
   func (r *VNCRelay) Close() error
   func (r *VNCRelay) Subscribe() <-chan relay.StatusEvent
   func (r *VNCRelay) ActivityChan() <-chan struct{}
   ```

   **options.go:**
   ```go
   type Options struct {
       DialTimeout    time.Duration
       ReadBufferSize int
       WriteBufferSize int
   }
   ```

2. **Implement bidirectional relay**
   - WebSocket binary frames ↔ TCP VNC stream
   - Handle connection lifecycle
   - Emit status events (connecting, connected, disconnected)

3. **Write tests** (`server/internal/gateway/vnc/relay_test.go`)
   - Mock VNC server for testing
   - Test connection establishment
   - Test bidirectional data flow
   - Test error handling and cleanup

4. **Update Manager** (`server/internal/gateway/manager/manager.go`)
   - Add `createVNCRelay` method
   - Update relay creation logic to check project `GUIEnabled`

### Deliverables
- [ ] VNCRelay implements `relay.Proxier` interface
- [ ] Bidirectional WebSocket ↔ TCP relay working
- [ ] Unit tests with >80% coverage
- [ ] Manager can create VNC relays for GUI-enabled projects

---

## Phase 3: Gateway API & WebSocket Endpoint

**Duration:** ~3-4 hours

### Tasks

1. **Add `/vnc` WebSocket endpoint** (`server/cmd/gateway/main.go`)
   ```go
   mux.HandleFunc("GET /vnc", handleVNCWebSocket)
   ```

2. **Implement VNC WebSocket handler**
   - Validate `tab` query parameter
   - Verify project has GUI enabled
   - Verify tab ownership (same as terminal)
   - Upgrade to WebSocket with binary subprotocol
   - Proxy via VNCRelay

3. **Update project API responses**
   - Include `guiEnabled`, `guiResolution`, `guiVNCPort` in `/api/projects`
   - Include in `/api/admin/projects`

4. **Update Admin API** (`server/internal/handlers/admin/projects.go`)
   - Accept GUI fields in create/update requests
   - Validate resolution format
   - Validate VNC port range

### Deliverables
- [ ] `/vnc?tab=<id>` endpoint working
- [ ] Project APIs return GUI fields
- [ ] Admin API accepts GUI configuration
- [ ] Integration tests pass

---

## Phase 4: Project Pod GUI Stack

**Duration:** ~6-8 hours

### Tasks

1. **Update Dockerfile** - Add GUI packages
   ```dockerfile
   # GUI dependencies
   RUN apt-get update && apt-get install -y --no-install-recommends \
       supervisor \
       xvfb \
       x11vnc \
       xfce4 \
       xfce4-terminal \
       thunar \
       dbus-x11 \
       fonts-liberation \
       fonts-dejavu-core \
       chromium \
       firefox-esr
   ```

2. **Create supervisor configuration** (`config/supervisor/kubetty.conf`)
   - kubetty-project program
   - Xvfb program (conditional on GUI_ENABLED)
   - x11vnc program (conditional)
   - XFCE session program (conditional)

3. **Create GUI startup script** (`scripts/start-gui.sh`)
   - Start Xvfb with configured resolution
   - Start x11vnc connected to Xvfb
   - Start XFCE session
   - Health check for each component

4. **Create shell integration** (`scripts/kubetty-gui.sh`)
   - Export DISPLAY when GUI enabled
   - Add aliases for common GUI apps with hints
   - `_kubetty_gui_hint` function

5. **Create XFCE default configuration** (`config/xfce/`)
   - Panel layout
   - Default applications
   - Theme settings

6. **Update entrypoint** to use supervisor when GUI enabled

7. **Add `/api/gui/status` endpoint** (project pod)
   - Return GUI stack health
   - List running X11 applications

### Deliverables
- [ ] GUI stack starts when GUI_ENABLED=true
- [ ] Xvfb, x11vnc, XFCE all running
- [ ] VNC accessible on configured port
- [ ] Shell hints work when launching GUI apps
- [ ] `/api/gui/status` returns correct info

---

## Phase 5: Frontend - Core Components

**Duration:** ~6-8 hours

### Tasks

1. **Install noVNC package**
   ```bash
   cd web && npm install @novnc/novnc
   ```

2. **Create type declarations** (`web/src/types/novnc.d.ts`)
   - RFB class types
   - Event types

3. **Create SplitPane component** (`web/src/components/SplitPane.tsx`)
   - Horizontal and vertical modes
   - Draggable resizer
   - Minimum size constraints
   - Mouse and touch support
   - CSS in `SplitPane.css`

4. **Create GUIView component** (`web/src/components/GUIView.tsx`)
   - noVNC RFB wrapper
   - Connection lifecycle management
   - Scale viewport option
   - Status callbacks (connecting, connected, disconnected)
   - CSS in `GUIView.css`

5. **Create ViewToolbar component** (`web/src/components/ViewToolbar.tsx`)
   - Terminal/GUI/Split mode buttons
   - Split dropdown menu (horizontal/vertical)
   - Active mode indicator
   - Disabled state when GUI not available
   - CSS in `ViewToolbar.css`

6. **Write component tests**
   - SplitPane resize behavior
   - ViewToolbar mode switching
   - GUIView connection handling

### Deliverables
- [ ] SplitPane renders and resizes correctly
- [ ] GUIView connects to VNC via noVNC
- [ ] ViewToolbar switches modes
- [ ] All components have tests
- [ ] Responsive on different screen sizes

---

## Phase 6: Frontend - Integration

**Duration:** ~4-6 hours

### Tasks

1. **Create/Update TabView component** (`web/src/components/TabView.tsx`)
   - ViewMode state management
   - Render correct view based on mode
   - SplitPane integration for split modes
   - Pass guiEnabled from project info

2. **Add keyboard shortcuts**
   - `Ctrl+Shift+T` - Terminal only
   - `Ctrl+Shift+G` - GUI only
   - `Ctrl+Shift+S` - Toggle split
   - Handle focus management

3. **Add localStorage persistence**
   - Save view mode per tab
   - Save split ratio per tab
   - Restore on tab activation

4. **Update TabBar** (`web/src/components/TabBar.tsx`)
   - Show GUI indicator for GUI-enabled projects
   - Optional: different icon for GUI tabs

5. **Add GUI hint notification**
   - Detect "GUI app launched" in terminal output
   - Show floating notification with "Open GUI" button
   - Auto-dismiss after timeout

6. **Update project creation form** (Admin UI)
   - Add GUI enable checkbox
   - Add resolution selector
   - Show resource requirements warning

### Deliverables
- [ ] View modes switch correctly
- [ ] Split view resizes and persists
- [ ] Keyboard shortcuts work
- [ ] GUI hint appears when app launched
- [ ] Admin UI can enable GUI for projects

---

## Phase 7: Helm & Configuration

**Duration:** ~2-3 hours

### Tasks

1. **Update Helm values** (`deploy/helm/values.yaml`)
   ```yaml
   gui:
     enabled: false
     resolution: "1920x1080x24"
     vncPort: 5901
     desktop: "xfce"
   ```

2. **Update deployment template** (`deploy/helm/templates/deployment.yaml`)
   - Add GUI environment variables
   - Conditional supervisor vs direct entrypoint
   - Resource adjustments when GUI enabled

3. **Add NetworkPolicy for VNC** (`deploy/helm/templates/networkpolicy.yaml`)
   - Allow gateway → project pod on VNC port

4. **Update project controller** (`server/internal/controller/`)
   - Pass GUI settings to project deployments
   - Set GUI_ENABLED, GUI_RESOLUTION, VNC_PORT env vars

5. **Update documentation**
   - Helm values reference
   - Resource requirements
   - Troubleshooting guide

### Deliverables
- [ ] Helm chart deploys GUI-enabled projects
- [ ] NetworkPolicy allows VNC traffic
- [ ] Controller passes GUI settings correctly
- [ ] Documentation updated

---

## Phase 8: Testing & Polish

**Duration:** ~4-6 hours

### Tasks

1. **End-to-end testing**
   - Create GUI-enabled project via Admin API
   - Open terminal tab
   - Run Firefox from terminal
   - Switch to GUI view
   - Verify Firefox visible
   - Test split view resizing
   - Test keyboard shortcuts

2. **Performance testing**
   - VNC bandwidth measurement
   - Latency testing
   - Memory usage profiling

3. **Browser compatibility**
   - Chrome, Firefox, Safari, Edge
   - Mobile browsers (iOS Safari, Android Chrome)

4. **Error handling**
   - VNC connection failures
   - GUI stack not starting
   - Resolution mismatch

5. **Polish**
   - Loading states
   - Error messages
   - Reconnection behavior
   - Fullscreen mode

### Deliverables
- [ ] E2E tests pass
- [ ] Works on major browsers
- [ ] Error states handled gracefully
- [ ] Performance acceptable (<100ms latency)

---

## Implementation Order Summary

| Phase | Description | Est. Hours | Dependencies |
|-------|-------------|------------|--------------|
| 1 | Database & Data Model | 2-3 | None |
| 2 | Gateway VNC Relay | 4-6 | Phase 1 |
| 3 | Gateway API & WebSocket | 3-4 | Phase 2 |
| 4 | Project Pod GUI Stack | 6-8 | None (parallel) |
| 5 | Frontend Core Components | 6-8 | None (parallel) |
| 6 | Frontend Integration | 4-6 | Phase 3, 5 |
| 7 | Helm & Configuration | 2-3 | Phase 4 |
| 8 | Testing & Polish | 4-6 | All |

**Total Estimated Time:** 32-44 hours

**Parallelization:** Phases 4 and 5 can run in parallel with Phases 1-3.

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| noVNC performance issues | Test early with real workloads; consider compression settings |
| XFCE resource usage | Profile memory; consider lighter alternatives (LXDE) as fallback |
| Browser compatibility | Test on target browsers early in Phase 5 |
| VNC security | Rely on gateway auth + NetworkPolicy; document threat model |
| Complex state management | Use React context for view state; extensive testing |

---

## Success Criteria

1. User can create a project with GUI enabled
2. User can open a terminal tab and run `firefox`
3. User sees hint message about GUI app
4. User clicks "GUI" or "Split" to see Firefox
5. Split view is resizable via drag
6. View preferences persist across sessions
7. Keyboard shortcuts work as documented
8. Performance is acceptable (< 100ms latency, < 5 Mbps bandwidth for static screens)
