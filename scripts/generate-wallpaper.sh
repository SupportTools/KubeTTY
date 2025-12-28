#!/bin/bash
# generate-wallpaper.sh - Generate KubeTTY wallpaper with project info
#
# Uses custom wallpaper base image and adds text overlay:
# - Scales wallpaper to target resolution
# - Adds Project name, User, and Version at bottom
#
# Environment variables used:
#   KUBETTY_PROJECT - Project name (defaults to "KubeTTY")
#   KUBETTY_USER    - User name (defaults to $USER)
#   KUBETTY_VERSION - Version string (defaults to reading /etc/kubetty-version)
#   GUI_RESOLUTION  - Resolution in WIDTHxHEIGHTxDEPTH format (defaults to 1920x1080x24)

set -euo pipefail

# Parse resolution (format: WIDTHxHEIGHTxDEPTH)
RESOLUTION="${GUI_RESOLUTION:-1920x1080x24}"
WIDTH=$(echo "$RESOLUTION" | cut -d'x' -f1)
HEIGHT=$(echo "$RESOLUTION" | cut -d'x' -f2)

# Project info
PROJECT="${KUBETTY_PROJECT:-KubeTTY}"
USERNAME="${KUBETTY_USER:-${USER:-mmattox}}"

# Version info (from env var or version file)
if [ -n "${KUBETTY_VERSION:-}" ]; then
    VERSION="$KUBETTY_VERSION"
elif [ -f /etc/kubetty-version ]; then
    VERSION=$(cat /etc/kubetty-version)
else
    VERSION="dev"
fi

# Paths
WALLPAPER_BASE="/usr/share/kubetty/wallpaper.png"
LOGO_PATH="/usr/share/kubetty/logo.png"
OUTPUT_PATH="/home/mmattox/.local/share/backgrounds/kubetty-wallpaper.png"

# Ensure output directory exists
mkdir -p "$(dirname "$OUTPUT_PATH")"

# Text color (light gray for readability on dark background)
TEXT_COLOR="#9ca3af"

log() {
    echo "[generate-wallpaper] $(date '+%Y-%m-%d %H:%M:%S') $*"
}

log "Generating wallpaper: ${WIDTH}x${HEIGHT}"
log "  Project: $PROJECT"
log "  User: $USERNAME"
log "  Version: $VERSION"

# Check if custom wallpaper base exists
if [ -f "$WALLPAPER_BASE" ]; then
    log "Using custom wallpaper base: $WALLPAPER_BASE"

    # Scale wallpaper to target resolution and add text overlay
    convert "$WALLPAPER_BASE" \
        -resize "${WIDTH}x${HEIGHT}^" \
        -gravity center -extent "${WIDTH}x${HEIGHT}" \
        -gravity south \
        -font DejaVu-Sans -pointsize 18 -fill "$TEXT_COLOR" \
        -annotate +0+80 "Project: $PROJECT" \
        -font DejaVu-Sans -pointsize 14 -fill "$TEXT_COLOR" \
        -annotate +0+55 "User: $USERNAME" \
        -font DejaVu-Sans -pointsize 12 -fill "$TEXT_COLOR" \
        -annotate +0+30 "Version: $VERSION" \
        "$OUTPUT_PATH"
elif [ -f "$LOGO_PATH" ]; then
    log "Wallpaper base not found, falling back to logo: $LOGO_PATH"

    # Fallback: dark background with centered logo
    BG_COLOR="#1a1a2e"
    TARGET_LOGO_WIDTH=$((WIDTH * 35 / 100))

    convert -size "${WIDTH}x${HEIGHT}" "xc:${BG_COLOR}" \
        \( "$LOGO_PATH" -resize "${TARGET_LOGO_WIDTH}x" -background none \) \
        -gravity center -composite \
        -gravity south \
        -font DejaVu-Sans -pointsize 18 -fill "$TEXT_COLOR" \
        -annotate +0+80 "Project: $PROJECT" \
        -font DejaVu-Sans -pointsize 14 -fill "$TEXT_COLOR" \
        -annotate +0+55 "User: $USERNAME" \
        -font DejaVu-Sans -pointsize 12 -fill "$TEXT_COLOR" \
        -annotate +0+30 "Version: $VERSION" \
        "$OUTPUT_PATH"
else
    log "WARNING: No wallpaper or logo found, creating text-only wallpaper"

    BG_COLOR="#1a1a2e"
    ACCENT_COLOR="#60a5fa"

    convert -size "${WIDTH}x${HEIGHT}" "xc:${BG_COLOR}" \
        -gravity center \
        -font DejaVu-Sans-Bold -pointsize 72 -fill "$ACCENT_COLOR" \
        -annotate +0-50 "KubeTTY" \
        -font DejaVu-Sans -pointsize 24 -fill "$TEXT_COLOR" \
        -annotate +0+50 "Project: $PROJECT" \
        -gravity south \
        -font DejaVu-Sans -pointsize 16 -fill "$TEXT_COLOR" \
        -annotate +0+30 "User: $USERNAME | Version: $VERSION" \
        "$OUTPUT_PATH"
fi

log "Wallpaper generated: $OUTPUT_PATH"

# Verify output
if [ -f "$OUTPUT_PATH" ]; then
    SIZE=$(stat -c%s "$OUTPUT_PATH" 2>/dev/null || stat -f%z "$OUTPUT_PATH" 2>/dev/null || echo "unknown")
    log "Output file size: $SIZE bytes"
else
    log "ERROR: Failed to generate wallpaper"
    exit 1
fi
