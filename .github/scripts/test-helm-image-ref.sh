#!/usr/bin/env bash
set -euo pipefail

chart="deploy/helm-gateway"
repository="harbor.support.tools/kubetty/kubetty"
tag="test-tag"
digest="sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

tag_render=$(helm template gateway "$chart" \
  --set-string image.repository="$repository" \
  --set-string image.tag="$tag")
if ! grep -Fq "image: \"${repository}:${tag}\"" <<<"$tag_render"; then
  echo "ERROR: gateway chart did not render the tag fallback"
  exit 1
fi

digest_render=$(helm template gateway "$chart" \
  --set-string image.repository="$repository" \
  --set-string image.tag="ignored-when-digest-is-set" \
  --set-string image.digest="$digest")
if ! grep -Fq "image: \"${repository}@${digest}\"" <<<"$digest_render"; then
  echo "ERROR: gateway chart did not render the immutable digest reference"
  exit 1
fi
if grep -Fq "${repository}:ignored-when-digest-is-set" <<<"$digest_render"; then
  echo "ERROR: gateway chart rendered a tag even though a digest was set"
  exit 1
fi

error_log=$(mktemp)
trap 'rm -f "$error_log"' EXIT
if helm template gateway "$chart" \
  --set-string image.digest="sha256:not-a-valid-digest" \
  > /dev/null 2>"$error_log"; then
  echo "ERROR: gateway chart accepted a malformed image digest"
  exit 1
fi
if ! grep -Fq "invalid image digest" "$error_log"; then
  echo "ERROR: malformed digest failed without the expected validation message"
  cat "$error_log"
  exit 1
fi

echo "Gateway image reference tests passed"
