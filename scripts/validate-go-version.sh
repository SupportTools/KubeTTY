#!/bin/bash
#
# validate-go-version.sh - Verify Go version meets requirements
#
# This script checks that the installed Go version matches the required version.
# It can be run standalone or sourced by other validation scripts.
#
# Usage:
#   ./scripts/validate-go-version.sh [REQUIRED_VERSION]
#
# Arguments:
#   REQUIRED_VERSION - Optional. Go version to require (default: 1.23)
#
# Exit codes:
#   0 - Go version matches requirements
#   1 - Go version mismatch or Go not installed
#
# Examples:
#   ./scripts/validate-go-version.sh        # Uses default 1.23
#   ./scripts/validate-go-version.sh 1.23   # Explicitly require 1.23.x

set -eu

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
REQUIRED_VERSION="${1:-1.23}"

print_error() {
    echo -e "${RED}[go-version] ERROR: $1${NC}" >&2
}

print_success() {
    echo -e "${GREEN}[go-version] $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}[go-version] WARNING: $1${NC}"
}

# Check if Go is installed
check_go_installed() {
    if ! command -v go &>/dev/null; then
        print_error "Go is not installed"
        echo ""
        echo "Please install Go ${REQUIRED_VERSION}.x from https://go.dev/dl/"
        return 1
    fi
    return 0
}

# Get installed Go version
get_go_version() {
    go version | awk '{print $3}' | sed 's/go//'
}

# Check if version matches requirement
check_version_match() {
    local installed_version="$1"
    local required_version="$2"

    # Extract major.minor from installed version (e.g., "1.23.3" -> "1.23")
    local installed_major_minor
    installed_major_minor=$(echo "$installed_version" | cut -d'.' -f1,2)

    if [[ "$installed_major_minor" == "$required_version" ]]; then
        return 0
    else
        return 1
    fi
}

# Main validation
main() {
    echo "[go-version] Checking Go version..."

    # Check Go is installed
    if ! check_go_installed; then
        exit 1
    fi

    # Get installed version
    local installed_version
    installed_version=$(get_go_version)

    # Check version match
    if check_version_match "$installed_version" "$REQUIRED_VERSION"; then
        print_success "Go version ${installed_version} matches required ${REQUIRED_VERSION}.x"
        exit 0
    else
        print_error "Go version mismatch"
        echo ""
        echo "Installed: ${installed_version}"
        echo "Required:  ${REQUIRED_VERSION}.x"
        echo ""
        echo "Please install Go ${REQUIRED_VERSION}.x from https://go.dev/dl/"
        echo ""
        echo "If using asdf:"
        echo "  asdf install golang ${REQUIRED_VERSION}.3"
        echo "  asdf local golang ${REQUIRED_VERSION}.3"
        exit 1
    fi
}

# Run main if script is executed (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
