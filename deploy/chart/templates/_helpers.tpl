{{/* SPDX-License-Identifier: Apache-2.0 */}}
{{/* リリース名が dpf-webhook を含めば重ねない (例: リリース dpf-webhook → dpf-webhook)。 */}}
{{- define "webhook.fullname" -}}
{{- if contains "dpf-webhook" .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name "dpf-webhook" | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "webhook.labels" -}}
app.kubernetes.io/name: cert-manager-iij-dpf-webhook
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{- define "webhook.selectorLabels" -}}
app.kubernetes.io/name: cert-manager-iij-dpf-webhook
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "webhook.lockOwner" -}}
{{- required "lockOwner is required (1-32 characters: letters, digits, '.', '-', '_')" .Values.lockOwner -}}
{{- end -}}
