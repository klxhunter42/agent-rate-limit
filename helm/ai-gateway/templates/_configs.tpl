{{- define "ai-gateway.envVars" -}}
{{- range $key, $val := . }}
- name: {{ $key }}
  value: {{ $val | quote }}
{{- end }}
{{- end }}

{{- define "ai-gateway.envFromSecrets" -}}
{{- range $envName, $ref := . }}
- name: {{ $envName }}
  valueFrom:
    secretKeyRef:
      name: {{ $ref.secret }}
      key: {{ $ref.key }}
      {{- if $ref.optional }}
      optional: true
      {{- end }}
{{- end }}
{{- end }}
