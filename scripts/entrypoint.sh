#!/bin/bash
set -euo pipefail

# KubeTTY entrypoint script
# Selects which binary to run based on KUBETTY_MODE environment variable
#
# KUBETTY_MODE values:
#   gateway - Run in gateway mode (multi-project WebSocket relay)
#   project - Run in project mode (single PTY session)
#
# Defaults to 'gateway' for backward compatibility

MODE="${KUBETTY_MODE:-gateway}"

case "$MODE" in
  gateway)
    echo "Starting KubeTTY in gateway mode..."
    exec /usr/local/bin/kubetty-gateway "$@"
    ;;
  project)
    echo "Starting KubeTTY in project mode..."
    exec /usr/local/bin/kubetty-project "$@"
    ;;
  *)
    echo "Error: Invalid KUBETTY_MODE='$MODE'. Must be 'gateway' or 'project'." >&2
    exit 1
    ;;
esac
