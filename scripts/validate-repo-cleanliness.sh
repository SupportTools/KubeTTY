#!/bin/bash
#
# validate-repo-cleanliness.sh - Check for forbidden files in the repository
#
# This script scans staged files (or all tracked files) for patterns that
# should never be committed, such as private keys, credentials, and secrets.
#
# Usage:
#   ./scripts/validate-repo-cleanliness.sh [--staged|--all]
#
# Options:
#   --staged  Check only staged files (default, for pre-commit hooks)
#   --all     Check all tracked files in the repository
#
# Exit codes:
#   0 - No forbidden files detected
#   1 - Forbidden files detected
#
# The forbidden patterns are loaded from .validation.json if available,
# otherwise uses a hardcoded list.

set -eu

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get the root of the repository
REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo ".")

# Mode: staged (default) or all
MODE="${1:---staged}"

print_error() {
    echo -e "${RED}[repo-cleanliness] ERROR: $1${NC}" >&2
}

print_success() {
    echo -e "${GREEN}[repo-cleanliness] $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}[repo-cleanliness] WARNING: $1${NC}"
}

# Default forbidden patterns (used if .validation.json not found)
get_default_patterns() {
    cat << 'EOF'
\.key$
\.pem$
\.p12$
\.pfx$
\.p8$
\.jks$
\.keystore$
\.secrets\.yaml$
\.secrets\.yml$
^\.env$
\.env\.local$
\.env\.production$
\.env\.staging$
\.env\.development$
credentials\.json$
service-account\.json$
kubeconfig$
\.kube/config$
id_rsa$
id_ed25519$
id_ecdsa$
id_dsa$
\.ssh/.*_key$
\.aws/credentials$
\.gcp/credentials\.json$
EOF
}

# Load patterns from .validation.json if available
load_patterns() {
    local config_file="$REPO_ROOT/.validation.json"

    if [[ -f "$config_file" ]] && command -v jq &>/dev/null; then
        local patterns
        patterns=$(jq -r '.forbiddenFiles.patterns[]' "$config_file" 2>/dev/null)
        if [[ -n "$patterns" ]]; then
            echo "$patterns"
            return
        fi
    fi

    # Fall back to default patterns
    get_default_patterns
}

# Get list of files to check
get_files_to_check() {
    case "$MODE" in
        --staged)
            git diff --cached --name-only --diff-filter=ACM 2>/dev/null || true
            ;;
        --all)
            git ls-files 2>/dev/null || true
            ;;
        *)
            print_error "Unknown mode: $MODE"
            echo "Usage: $0 [--staged|--all]"
            exit 1
            ;;
    esac
}

# Check a single file against all patterns
check_file() {
    local file="$1"
    local patterns="$2"

    while IFS= read -r pattern; do
        if [[ -z "$pattern" ]]; then
            continue
        fi
        if echo "$file" | grep -qE "$pattern"; then
            echo "$file|$pattern"
            return 0
        fi
    done <<< "$patterns"

    return 1
}

# Main validation
main() {
    echo "[repo-cleanliness] Checking for forbidden files..."

    local mode_desc
    case "$MODE" in
        --staged)
            mode_desc="staged files"
            ;;
        --all)
            mode_desc="all tracked files"
            ;;
    esac
    echo "[repo-cleanliness] Mode: $mode_desc"

    # Load patterns
    local patterns
    patterns=$(load_patterns)

    # Get files to check
    local files
    files=$(get_files_to_check)

    if [[ -z "$files" ]]; then
        print_success "No files to check"
        exit 0
    fi

    # Track violations
    local violations=0
    local violation_list=""

    # Check each file
    while IFS= read -r file; do
        if [[ -z "$file" ]]; then
            continue
        fi

        local result
        if result=$(check_file "$file" "$patterns"); then
            violations=$((violations + 1))
            violation_list="${violation_list}${result}\n"
        fi
    done <<< "$files"

    # Report results
    if [[ $violations -gt 0 ]]; then
        print_error "Found $violations forbidden file(s):"
        echo ""
        echo -e "$violation_list" | while IFS='|' read -r file pattern; do
            if [[ -n "$file" ]]; then
                echo "  - $file"
                echo "    Matches pattern: $pattern"
            fi
        done
        echo ""
        echo "These files should not be committed to the repository."
        echo "If this is intentional, you can:"
        echo "  1. Add the file to .gitignore"
        echo "  2. Use --no-verify to bypass (not recommended)"
        echo ""
        exit 1
    fi

    print_success "No forbidden files detected"
    exit 0
}

# Run main if script is executed (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
