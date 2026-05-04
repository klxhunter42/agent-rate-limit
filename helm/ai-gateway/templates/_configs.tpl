{{- define "ai-gateway.envVars" -}}
{{- range $key, $val := . -}}
- name: {{ $key }}
  value: {{ $val | quote }}
{{ end -}}
{{- end -}}

{{- define "ai-gateway.envFromSecrets" -}}
{{- range $envName, $ref := . -}}
- name: {{ $envName }}
  valueFrom:
    secretKeyRef:
      name: {{ $ref.secret }}
      key: {{ $ref.key }}
{{- if $ref.optional }}
      optional: true
{{- end -}}
{{ end -}}
{{- end -}}

{{- define "ai-gateway.secretKeyRef" -}}
{{- $secretName := "ai-gateway-secrets" -}}
{{- $secretKey := .key -}}
{{- if kindIs "map" .value -}}
{{- $secretName = .value.secret -}}
{{- $secretKey = .value.key -}}
{{- end -}}
name: {{ $secretName }}
key: {{ $secretKey }}
optional: true
{{- end -}}
