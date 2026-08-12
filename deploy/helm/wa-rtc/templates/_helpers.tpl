{{/* Common labels for every RTC object. */}}
{{- define "wa-rtc.labels" -}}
app.kubernetes.io/part-of: whatsapp-v2
app.kubernetes.io/component: rtc-media
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* Per-component selector labels (livekit | coturn). */}}
{{- define "wa-rtc.selector" -}}
app.kubernetes.io/instance: {{ .instance }}
app.kubernetes.io/name: {{ .name }}
{{- end -}}

{{/* Dedicated media node pool: nodeSelector + tolerations (rtc-lld §1/§5). */}}
{{- define "wa-rtc.nodePool" -}}
{{- with .Values.nodePool.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.nodePool.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}
