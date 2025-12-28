#!/bin/bash
# start-gui.sh - Start GUI stack components via supervisor
#
# This script is called by the entrypoint when GUI_ENABLED=true.
# It configures environment variables and starts the GUI program group.

set -euo pipefail

# Supervisor config file path
SUPERVISOR_CONF="/etc/supervisor/conf.d/kubetty.conf"

# Helper function to call supervisorctl with our config
supervisorctl_cmd() {
    supervisorctl -c "$SUPERVISOR_CONF" "$@"
}

# Default values if not set
export GUI_RESOLUTION="${GUI_RESOLUTION:-1920x1080x24}"
export GUI_VNC_PORT="${GUI_VNC_PORT:-5900}"

log() {
    echo "[start-gui] $(date '+%Y-%m-%d %H:%M:%S') $*"
}

log "Starting GUI stack..."
log "  Resolution: $GUI_RESOLUTION"
log "  VNC Port:   $GUI_VNC_PORT"

# Create XDG runtime directory for D-Bus
XDG_RUNTIME_DIR="/run/user/$(id -u)"
if [ ! -d "$XDG_RUNTIME_DIR" ]; then
    sudo mkdir -p "$XDG_RUNTIME_DIR"
    sudo chown "$(id -u):$(id -g)" "$XDG_RUNTIME_DIR"
    sudo chmod 700 "$XDG_RUNTIME_DIR"
fi
export XDG_RUNTIME_DIR

# Wait for supervisor to be ready
# Note: supervisorctl returns exit 0 if all programs running, 3 if some stopped, 4 if cannot connect
# We consider 0-3 as "ready" (connected), only 4 means not ready
max_attempts=30
attempt=0
while [ $attempt -lt $max_attempts ]; do
    # Use || ret=$? pattern to prevent errexit from triggering on non-zero exit codes
    ret=0
    supervisorctl_cmd status >/dev/null 2>&1 || ret=$?
    if [ $ret -ne 4 ]; then
        # Exit codes 0-3 mean we connected successfully
        break
    fi
    attempt=$((attempt + 1))
    sleep 0.5
done

if [ $attempt -eq $max_attempts ]; then
    log "ERROR: Supervisor not ready after $max_attempts attempts"
    exit 1
fi

# Start GUI program group (programs are in 'gui' group, must use gui:program prefix)
log "Starting Xvfb..."
supervisorctl_cmd start gui:xvfb

# Wait for Xvfb to be ready
sleep 2
attempt=0
while [ $attempt -lt 20 ]; do
    if [ -e "/tmp/.X11-unix/X99" ]; then
        break
    fi
    attempt=$((attempt + 1))
    sleep 0.5
done

if [ ! -e "/tmp/.X11-unix/X99" ]; then
    log "ERROR: Xvfb failed to create display socket"
    exit 1
fi

log "Xvfb ready, starting D-Bus..."
supervisorctl_cmd start gui:dbus
sleep 1

log "Starting x11vnc on port $GUI_VNC_PORT..."
supervisorctl_cmd start gui:x11vnc
sleep 1

# Generate custom wallpaper with project info before starting XFCE
if [ -x /usr/local/bin/generate-wallpaper ]; then
    log "Generating custom wallpaper..."
    /usr/local/bin/generate-wallpaper || log "WARNING: Wallpaper generation failed, using default"
fi

log "Starting XFCE desktop..."
supervisorctl_cmd start gui:xfce
sleep 3

# Apply XFCE settings using xfconf-query AFTER XFCE starts
# XFCE overwrites pre-seeded XML configs, so we must set them programmatically
log "Applying XFCE desktop settings..."

# Set wallpaper for all screens (XFCE uses per-screen/per-workspace settings)
WALLPAPER_PATH="/home/mmattox/.local/share/backgrounds/kubetty-wallpaper.png"
if [ -f "$WALLPAPER_PATH" ]; then
    log "  Setting custom wallpaper: $WALLPAPER_PATH"
    # Get all backdrop properties and set them
    for prop in $(DISPLAY=:99 xfconf-query -c xfce4-desktop -l 2>/dev/null | grep "last-image" || true); do
        DISPLAY=:99 xfconf-query -c xfce4-desktop -p "$prop" -s "$WALLPAPER_PATH" 2>/dev/null || true
    done
    # Also set the common backdrop properties that might exist
    DISPLAY=:99 xfconf-query -c xfce4-desktop -p /backdrop/screen0/monitor0/workspace0/last-image -s "$WALLPAPER_PATH" --create -t string 2>/dev/null || true
    DISPLAY=:99 xfconf-query -c xfce4-desktop -p /backdrop/screen0/monitorscreen/workspace0/last-image -s "$WALLPAPER_PATH" --create -t string 2>/dev/null || true
    # Set image style to stretched (5) to ensure wallpaper fills screen
    DISPLAY=:99 xfconf-query -c xfce4-desktop -p /backdrop/screen0/monitor0/workspace0/image-style -s 5 --create -t int 2>/dev/null || true
    DISPLAY=:99 xfconf-query -c xfce4-desktop -p /backdrop/screen0/monitorscreen/workspace0/image-style -s 5 --create -t int 2>/dev/null || true
    log "  ✓ Wallpaper configured"
else
    log "  WARNING: Wallpaper not found at $WALLPAPER_PATH"
fi

# Disable power management
log "  Disabling power management..."
DISPLAY=:99 xfconf-query -c xfce4-power-manager -p /xfce4-power-manager/dpms-enabled -s false --create -t bool 2>/dev/null || true
DISPLAY=:99 xfconf-query -c xfce4-power-manager -p /xfce4-power-manager/blank-on-ac -s 0 --create -t int 2>/dev/null || true
DISPLAY=:99 xfconf-query -c xfce4-power-manager -p /xfce4-power-manager/dpms-on-ac-sleep -s 0 --create -t uint 2>/dev/null || true
DISPLAY=:99 xfconf-query -c xfce4-power-manager -p /xfce4-power-manager/dpms-on-ac-off -s 0 --create -t uint 2>/dev/null || true
DISPLAY=:99 xfconf-query -c xfce4-power-manager -p /xfce4-power-manager/inactivity-on-ac -s 0 --create -t uint 2>/dev/null || true
log "  ✓ Power management disabled"

# Disable screensaver
log "  Disabling screensaver..."
DISPLAY=:99 xfconf-query -c xfce4-screensaver -p /saver/enabled -s false --create -t bool 2>/dev/null || true
DISPLAY=:99 xfconf-query -c xfce4-screensaver -p /lock/enabled -s false --create -t bool 2>/dev/null || true
log "  ✓ Screensaver disabled"

# Disable session locking
log "  Disabling session locking..."
DISPLAY=:99 xfconf-query -c xfce4-session -p /general/LockCommand -s "" --create -t string 2>/dev/null || true
DISPLAY=:99 xfconf-query -c xfce4-session -p /shutdown/LockScreen -s false --create -t bool 2>/dev/null || true
log "  ✓ Session locking disabled"

# Kill power management and screensaver daemons (they can override settings)
log "  Killing power management daemons..."
pkill -f xfce4-power-manager 2>/dev/null || true
pkill -f light-locker 2>/dev/null || true
pkill -f xscreensaver 2>/dev/null || true
pkill -f xfce4-screensaver 2>/dev/null || true
log "  ✓ Power management daemons killed"

# Use xset to disable DPMS at X server level (most reliable method)
# This disables screen blanking and power saving at the X11 level
log "  Disabling DPMS at X server level..."
DISPLAY=:99 xset s off 2>/dev/null || true           # Disable screen saver
DISPLAY=:99 xset s noblank 2>/dev/null || true       # No screen blanking
DISPLAY=:99 xset -dpms 2>/dev/null || true           # Disable DPMS (Display Power Management Signaling)
DISPLAY=:99 xset s 0 0 2>/dev/null || true           # Set screensaver timeout to 0
log "  ✓ DPMS disabled at X server level"

log "XFCE settings applied"

# Verify all components are running
check_component() {
    local name="$1"
    local status
    # Use || true to prevent errexit from triggering on supervisorctl's non-zero exit code
    status=$(supervisorctl_cmd status "$name" 2>/dev/null | awk '{print $2}') || true
    if [ "$status" = "RUNNING" ]; then
        log "  ✓ $name is running"
        return 0
    else
        log "  ✗ $name failed ($status)"
        return 1
    fi
}

log "Verifying GUI stack health..."
failed=0
check_component "gui:xvfb" || failed=1
check_component "gui:dbus" || failed=1
check_component "gui:x11vnc" || failed=1
check_component "gui:xfce" || failed=1

if [ $failed -eq 0 ]; then
    log "GUI stack started successfully"
    log "VNC accessible at localhost:$GUI_VNC_PORT"
else
    log "WARNING: Some GUI components failed to start"
    exit 1
fi
