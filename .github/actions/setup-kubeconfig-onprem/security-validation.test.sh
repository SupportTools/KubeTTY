#!/usr/bin/env bash
# Unit tests for security-validation.sh. No Vault, no cluster, no network.
#
#   .github/actions/setup-kubeconfig-onprem/security-validation.test.sh
#
# These cover the cases where being wrong is SILENT: a lease that is really a Vault login
# lease, a denial that is really a timeout, an identity check that passes on empty output.
set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$DIR/security-validation.sh"

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n     %s\n' "$1" "${2:-}"; }
check(){ if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "expected '$3', got '$2'"; fi; }

echo "validate_mint_response"

validate_mint_response '{"lease_duration":3600,"data":{"service_account_token":"tok-abc"}}' \
  && check "accepts a well-formed 1h mint" "$ONPREM_SA_TOKEN/$ONPREM_SA_TOKEN_TTL" "tok-abc/3600" \
  || bad "accepts a well-formed 1h mint" "returned non-zero"

validate_mint_response '{"lease_duration":900,"data":{"service_account_token":"t"}}' \
  && ok "accepts a shorter lease" || bad "accepts a shorter lease" "returned non-zero"

# The one that matters: a Vault LOGIN response carries .auth.lease_duration and no
# top-level lease_duration. Reading the wrong field would report a healthy-looking TTL
# for a credential that is not the Kubernetes token at all.
validate_mint_response '{"auth":{"lease_duration":3600},"data":{"service_account_token":"tok"}}' \
  && bad "rejects .auth.lease_duration (login response)" "accepted it" \
  || ok "rejects .auth.lease_duration (login response)"

validate_mint_response '{"lease_duration":7200,"data":{"service_account_token":"tok"}}' \
  && bad "rejects a lease over 3600s" "accepted it" || ok "rejects a lease over 3600s"

validate_mint_response '{"lease_duration":0,"data":{"service_account_token":"tok"}}' \
  && bad "rejects a zero lease" "accepted it" || ok "rejects a zero lease"

validate_mint_response '{"lease_duration":"3600","data":{"service_account_token":"tok"}}' \
  && bad "rejects a string lease" "accepted it" || ok "rejects a string lease"

validate_mint_response '{"lease_duration":3600,"data":{}}' \
  && bad "rejects a missing token" "accepted it" || ok "rejects a missing token"

validate_mint_response '{"lease_duration":3600,"data":{"service_account_token":""}}' \
  && bad "rejects an empty token" "accepted it" || ok "rejects an empty token"

validate_mint_response '{"errors":["permission denied"]}' \
  && bad "rejects a Vault error body" "accepted it" || ok "rejects a Vault error body"

validate_mint_response 'curl: (22) The requested URL returned error: 403' \
  && bad "rejects non-JSON curl output" "accepted it" || ok "rejects non-JSON curl output"

validate_mint_response '' \
  && bad "rejects an empty response" "accepted it" || ok "rejects an empty response"

# A failed validation must not leave a token from a previous successful call lying around
# for the caller to use.
validate_mint_response '{"lease_duration":3600,"data":{"service_account_token":"first"}}' >/dev/null
validate_mint_response '{"errors":["nope"]}' >/dev/null
check "clears the token after a later failure" "${ONPREM_SA_TOKEN:-<empty>}" "<empty>"

echo "require_literal_denial"
require_literal_denial "no" "1"  && ok "accepts a real denial (no + status 1)" \
                                 || bad "accepts a real denial" "returned non-zero"
require_literal_denial "yes" "0" && bad "rejects an allow" "accepted it" || ok "rejects an allow"
# Every one of these exits non-zero. Treating status alone as proof would report an
# unreachable cluster as verified isolation.
require_literal_denial "" "1"    && bad "rejects empty output with status 1" "accepted it" \
                                 || ok "rejects empty output with status 1 (timeout looks like this)"
require_literal_denial "error: You must be logged in to the server (Unauthorized)" "1" \
  && bad "rejects an auth error" "accepted it" || ok "rejects an auth error with status 1"
require_literal_denial "no" "0"  && bad "rejects no with status 0" "accepted it" \
                                 || ok "rejects 'no' with status 0"

echo "require_expected_identity"
E="system:serviceaccount:arc-runners-supporttools:kubetty-ci-deployer"
require_expected_identity "$E" "$E" && ok "accepts the exact identity" \
                                    || bad "accepts the exact identity" "returned non-zero"
require_expected_identity "" "$E"   && bad "rejects empty actual" "accepted it" \
                                    || ok "rejects empty actual (whoami failed)"
require_expected_identity "system:anonymous" "$E" && bad "rejects anonymous" "accepted it" \
                                                  || ok "rejects a different identity"
require_expected_identity "system:serviceaccount:arc-runners-supporttools:beacon-support-ci-deployer" "$E" \
  && bad "rejects a sibling SA" "accepted it" || ok "rejects a sibling deployer SA"

echo
echo "passed=$PASS failed=$FAIL"
[ "$FAIL" -eq 0 ]

