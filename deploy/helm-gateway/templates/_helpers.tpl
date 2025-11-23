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
