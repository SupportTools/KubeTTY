#!/bin/bash
# kubetty-gui.sh - Shell integration for KubeTTY GUI mode
#
# This file is sourced in .bash_profile when GUI_ENABLED=true.
# It provides hints when launching GUI applications.

# Only configure if GUI is enabled
if [ "$GUI_ENABLED" = "true" ]; then
    # Set DISPLAY for X11 applications
    export DISPLAY=:99

    # Function to display hint after launching a GUI app
    _kubetty_gui_hint() {
        local app_name="$1"
        echo ""
        echo "╭──────────────────────────────────────────╮"
        echo "│  🖥  GUI Application Launched            │"
        echo "├──────────────────────────────────────────┤"
        echo "│  App: $app_name"
        echo "│  Display: $DISPLAY"
        echo "│                                          │"
        echo "│  Click [GUI] tab to view the desktop     │"
        echo "│  or use [Split] for side-by-side view    │"
        echo "╰──────────────────────────────────────────╯"
        echo ""
    }

    # Wrapper for Firefox
    firefox() {
        command firefox "$@" &
        _kubetty_gui_hint "Firefox"
    }

    # Wrapper for Chromium (requires --no-sandbox in container)
    chromium() {
        command chromium --no-sandbox "$@" &
        _kubetty_gui_hint "Chromium"
    }

    # Wrapper for chromium-browser (alternative name)
    chromium-browser() {
        command chromium-browser --no-sandbox "$@" &
        _kubetty_gui_hint "Chromium"
    }

    # Wrapper for XFCE terminal
    xfce4-terminal() {
        command xfce4-terminal "$@" &
        _kubetty_gui_hint "XFCE Terminal"
    }

    # Wrapper for Thunar file manager
    thunar() {
        command thunar "$@" &
        _kubetty_gui_hint "Thunar File Manager"
    }

    # Generic wrapper for any X11 application
    gui() {
        if [ -z "$1" ]; then
            echo "Usage: gui <command> [args...]"
            echo "Launches an X11 application on the GUI display"
            return 1
        fi
        "$@" &
        _kubetty_gui_hint "$1"
    }

    # Show GUI status
    gui-status() {
        echo "╭──────────────────────────────────────────╮"
        echo "│  KubeTTY GUI Status                      │"
        echo "├──────────────────────────────────────────┤"

        # Check Xvfb
        if pgrep -x Xvfb >/dev/null 2>&1; then
            echo "│  ✓ Xvfb:    Running (display $DISPLAY)    │"
        else
            echo "│  ✗ Xvfb:    Not running                 │"
        fi

        # Check x11vnc
        if pgrep -x x11vnc >/dev/null 2>&1; then
            local vnc_port="${GUI_VNC_PORT:-5900}"
            echo "│  ✓ x11vnc:  Running (port $vnc_port)       │"
        else
            echo "│  ✗ x11vnc:  Not running                 │"
        fi

        # Check XFCE
        if pgrep -f "xfce4-session" >/dev/null 2>&1; then
            echo "│  ✓ XFCE:    Running                      │"
        else
            echo "│  ✗ XFCE:    Not running                 │"
        fi

        # List running X11 apps
        echo "├──────────────────────────────────────────┤"
        echo "│  Running X11 Applications:              │"

        local apps=$(DISPLAY=:99 xdotool search --name "" 2>/dev/null | head -5)
        if [ -n "$apps" ]; then
            while IFS= read -r wid; do
                local name=$(DISPLAY=:99 xdotool getwindowname "$wid" 2>/dev/null | head -c 35)
                [ -n "$name" ] && echo "│    • $name"
            done <<< "$apps"
        else
            echo "│    (none)                                │"
        fi

        echo "╰──────────────────────────────────────────╯"
    }

    # Helpful message on shell start
    echo ""
    echo "╭──────────────────────────────────────────╮"
    echo "│  🖥  GUI Mode Enabled                    │"
    echo "├──────────────────────────────────────────┤"
    echo "│  DISPLAY=$DISPLAY"
    echo "│                                          │"
    echo "│  Commands:                               │"
    echo "│    gui <app>    - Launch any GUI app     │"
    echo "│    gui-status   - Show GUI stack status  │"
    echo "│    firefox      - Launch Firefox         │"
    echo "│    chromium     - Launch Chromium        │"
    echo "╰──────────────────────────────────────────╯"
    echo ""
fi
