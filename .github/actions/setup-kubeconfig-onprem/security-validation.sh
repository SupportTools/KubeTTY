#!/usr/bin/env bash
# shellcheck disable=SC2034 # Globals are consumed by the composite action after sourcing.
#
# Pure functions, no I/O, no credentials — so security-validation.test.sh can exercise
# every branch without Vault, a cluster, or a network. The composite action that sources
# this file cannot be unit tested; these can, and they hold the parts where being wrong
# is silent.

# Validate a Vault Kubernetes secrets-engine response WITHOUT printing it.
#
# The generated credential's lease is the TOP-LEVEL `.lease_duration`. An
# `.auth.lease_duration` describes a Vault *login* token instead, and reading that one
# would report a plausible-looking TTL that says nothing about the Kubernetes token the
# deploy actually uses.
#
# The upper bound is enforced here rather than trusted from the mount config: a Vault
# role change that widened the lease would otherwise silently hand CI a long-lived
# cluster credential, and nothing downstream would notice.
validate_mint_response() {
  local response="$1"
  local lease_duration token

  MINT_VALIDATION_ERROR=""
  ONPREM_SA_TOKEN=""
  ONPREM_SA_TOKEN_TTL=""

  if ! lease_duration="$(printf '%s' "$response" | jq -er \
    '.lease_duration | select(type == "number" and . == floor and . > 0 and . <= 3600) | tostring' \
    2>/dev/null)"; then
    MINT_VALIDATION_ERROR="the secrets-engine response must contain an integer top-level lease_duration from 1 through 3600 seconds"
    return 1
  fi

  if ! token="$(printf '%s' "$response" | jq -er \
    '.data.service_account_token | select(type == "string" and length > 0)' \
    2>/dev/null)"; then
    MINT_VALIDATION_ERROR="the secrets-engine response did not contain a ServiceAccount token"
    return 1
  fi

  ONPREM_SA_TOKEN="$token"
  ONPREM_SA_TOKEN_TTL="$lease_duration"
}

# `kubectl auth can-i` prints "yes" or "no" and exits 1 on a denial. Require BOTH
# signals.
#
# Treating any non-zero exit as proof of isolation is the trap: a timeout, a DNS failure,
# a TLS error and a killed process all exit non-zero too, so an unreachable cluster would
# be reported as "production is safely out of reach". The literal "no" is the only output
# that can only come from the authorizer.
require_literal_denial() {
  [ "${1-}" = "no" ] && [ "${2-}" = "1" ]
}

# The identity the kubeconfig must authenticate as. A wrong-cluster kubeconfig
# authenticates fine and then edits the wrong things, so compare the full username
# rather than merely checking that the call succeeded.
require_expected_identity() {
  local actual="${1-}" expected="${2-}"
  [ -n "$actual" ] && [ "$actual" = "$expected" ]
}

