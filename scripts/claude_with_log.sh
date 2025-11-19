#!/usr/bin/env bash

# Installs the claude_with_log helper and alias `c` inside the container image.
claude_with_log() {
  local timestamp log_dir log_file
  timestamp=$(date +"%Y%m%d_%H%M%S")
  log_dir="$HOME/claude_logs"
  mkdir -p "$log_dir"
  log_file="${log_dir}/claude_interactive_session_${timestamp}.log"

  echo "Starting interactive claude session."
  echo "All input and output will be logged to: $log_file"
  echo "To end the session and save the log, exit claude (Ctrl+C or exit)."
  echo "───────────────────────────────────────────────────────────────────────"

  script -q -c "~/.local/bin/claude --dangerously-skip-permissions" "$log_file"

  echo "───────────────────────────────────────────────────────────────────────"
  echo "Interactive claude session ended. Log saved to: $log_file"
}

alias c='claude_with_log'

# Persist bash history under the writable home PVC.
export HISTFILE="$HOME/.bash_history"
export HISTSIZE=10000
export HISTFILESIZE=20000
