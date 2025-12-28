#!/bin/bash
set -euo pipefail

# KubeTTY entrypoint script
# Selects which binary to run based on KUBETTY_MODE environment variable
#
# KUBETTY_MODE values:
#   gateway - Run in gateway mode (multi-project WebSocket relay)
#   project - Run in project mode (single PTY session)
#
# When GUI_ENABLED=true in project mode, uses supervisor to manage:
#   - kubetty-project
#   - Xvfb (virtual X server)
#   - x11vnc (VNC server)
#   - XFCE (desktop environment)
#
# Defaults to 'gateway' for backward compatibility

MODE="${KUBETTY_MODE:-gateway}"
GUI_ENABLED="${GUI_ENABLED:-false}"

log() {
    echo "[entrypoint] $(date '+%Y-%m-%d %H:%M:%S') $*"
}

case "$MODE" in
  gateway)
    log "Starting KubeTTY in gateway mode..."
    exec /usr/local/bin/kubetty-gateway "$@"
    ;;
  project)
    if [ "$GUI_ENABLED" = "true" ]; then
        log "Starting KubeTTY in project mode with GUI stack..."

        # Set default GUI environment if not specified
        # Note: Controller sets VNC_PORT, supervisor expects GUI_VNC_PORT
        export GUI_RESOLUTION="${GUI_RESOLUTION:-1920x1080x24}"
        export GUI_VNC_PORT="${VNC_PORT:-${GUI_VNC_PORT:-5901}}"

        log "  GUI Resolution: $GUI_RESOLUTION"
        log "  VNC Port: $GUI_VNC_PORT"

        # Sync Chrome from backup if /opt is mounted as PVC and Chrome is missing
        # Chrome is installed to /opt/google/chrome/ in Docker image but /opt is a PVC mount
        if [ -d /usr/local/share/chrome-backup/google ] && [ ! -f /opt/google/chrome/google-chrome ]; then
            log "  Syncing Chrome from backup (PVC mount detected)..."
            mkdir -p /opt/google
            cp -a /usr/local/share/chrome-backup/google/* /opt/google/
            log "  Chrome synced to /opt/google/chrome/"
        fi

        # Sync Playwright browsers from backup if missing (also in /opt)
        if [ -d /opt/ms-playwright-backup ] && [ ! -d /opt/ms-playwright/chromium-* ]; then
            log "  Syncing Playwright browsers from backup..."
            mkdir -p /opt/ms-playwright
            cp -a /opt/ms-playwright-backup/* /opt/ms-playwright/ 2>/dev/null || true
            log "  Playwright browsers synced"
        fi

        # Use supervisor to manage all processes
        exec /usr/bin/supervisord -c /etc/supervisor/conf.d/kubetty.conf
    else
        log "Starting KubeTTY in project mode..."
        exec /usr/local/bin/kubetty-project "$@"
    fi
    ;;
  *)
    log "Error: Invalid KUBETTY_MODE='$MODE'. Must be 'gateway' or 'project'."
    exit 1
    ;;
esac
