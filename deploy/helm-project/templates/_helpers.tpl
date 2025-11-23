{{- define "kubetty-project.name" -}}
project
{{- end -}}

{{- define "kubetty-project.fullname" -}}
project
{{- end -}}

{{- define "kubetty-project.serviceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{ .Values.serviceAccount.name }}
{{- else -}}
project
{{- end -}}
{{- end -}}

{{- define "kubetty-project.labels" -}}
app.kubernetes.io/name: project
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion }}
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/component: project
{{- end -}}

{{- define "kubetty-project.selectorLabels" -}}
app.kubernetes.io/name: project
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
