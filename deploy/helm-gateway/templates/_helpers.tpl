{{- define "kubetty-gateway.name" -}}
gateway
{{- end -}}

{{- define "kubetty-gateway.fullname" -}}
gateway
{{- end -}}

{{- define "kubetty-gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{ .Values.serviceAccount.name }}
{{- else -}}
gateway
{{- end -}}
{{- end -}}

{{- define "kubetty-gateway.labels" -}}
app.kubernetes.io/name: kubetty-gateway
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/component: gateway
{{- end -}}

{{- define "kubetty-gateway.selectorLabels" -}}
app.kubernetes.io/name: kubetty-gateway
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Render an immutable image reference when CI supplies a digest. The tag remains
available as a fallback for local installs and Helm revisions created before
digest pinning. Fail during rendering instead of allowing a malformed digest to
surface later as ImagePullBackOff.
*/}}
{{- define "kubetty-gateway.imageRef" -}}
{{- $repository := required "kubetty-gateway.imageRef: image.repository is required" .Values.image.repository -}}
{{- $digest := .Values.image.digest | default "" | toString | trim -}}
{{- if $digest -}}
{{- if not (regexMatch "^sha256:[0-9a-f]{64}$" $digest) -}}
{{- fail (printf "kubetty-gateway.imageRef: invalid image digest %q (expected sha256:<64 lowercase hex>)" $digest) -}}
{{- end -}}
{{- printf "%s@%s" $repository $digest -}}
{{- else -}}
{{- $tag := required "kubetty-gateway.imageRef: image.tag is required when image.digest is empty" .Values.image.tag -}}
{{- printf "%s:%s" $repository ($tag | toString) -}}
{{- end -}}
{{- end -}}
