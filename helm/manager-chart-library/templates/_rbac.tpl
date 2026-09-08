{{- define "manager-chart-library.operatorserviceaccount" -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Values.operatorserviceaccount.name }}
  namespace: {{ .Values.operatorserviceaccount.namespace }}

{{- end -}}

{{- define "manager-chart-library.agentserviceaccount" -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Values.agentserviceaccount.name }}
  namespace: {{ .Values.agentserviceaccount.namespace }}

{{- end -}}

{{- define "manager-chart-library.leaderelectionrole" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ .Values.leaderelectionrole.name }}
  namespace: {{ .Values.leaderelectionrole.namespace }}
rules:
{{ toYaml .Values.leaderelectionrole.rules | indent 0 }}

{{- end -}}

{{- define "manager-chart-library.leaderelectionrolebinding" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ .Values.leaderelectionrolebinding.name }}
  namespace: {{ .Values.leaderelectionrolebinding.namespace }}
subjects:
- kind: ServiceAccount
  name: {{ .Values.leaderelectionrolebinding.serviceaccount.name }}
  namespace: {{ .Values.leaderelectionrolebinding.serviceaccount.namespace }}
roleRef:
  kind: Role
  name: {{ .Values.leaderelectionrolebinding.rolename }}
  apiGroup: rbac.authorization.k8s.io

{{- end -}}

{{- define "manager-chart-library.daemonsetrole" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ .Values.daemonsetrole.name }}
  namespace: {{ .Values.daemonsetrole.namespace }}
rules:
{{ toYaml .Values.daemonsetrole.rules | indent 0 }}

{{- end -}}

{{- define "manager-chart-library.daemonsetrolebinding" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ .Values.daemonsetrolebinding.name }}
  namespace: {{ .Values.daemonsetrolebinding.namespace }}
subjects:
- kind: ServiceAccount
  name: {{ .Values.daemonsetrolebinding.serviceaccount.name }}
  namespace: {{ .Values.daemonsetrolebinding.serviceaccount.namespace }}
roleRef:
  kind: Role
  name: {{ .Values.daemonsetrolebinding.rolename }}
  apiGroup: rbac.authorization.k8s.io

{{- end -}}

{{- define "manager-chart-library.operatorclusterrole" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ .Values.operatorclusterrole.name }}
rules:
{{ toYaml .Values.operatorclusterrole.rules | indent 0 }}
{{- if .Values.ocp }}
- apiGroups:
  - security.openshift.io
  resourceNames:
  - privileged
  resources:
  - securitycontextconstraints
  verbs:
  - use
{{- end -}}

{{- end -}}

{{- define "manager-chart-library.operatorclusterrolebinding" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ .Values.operatorclusterrolebinding.name }}
subjects:
- kind: ServiceAccount
  name: {{ .Values.operatorclusterrolebinding.serviceaccount.name }}
  namespace: {{ .Values.operatorclusterrolebinding.serviceaccount.namespace }}
roleRef:
  kind: ClusterRole
  name: {{ .Values.operatorclusterrolebinding.clusterrolename }}
  apiGroup: rbac.authorization.k8s.io

{{- end -}}

{{- define "manager-chart-library.agentclusterrole" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ .Values.agentclusterrole.name }}
rules:
{{ toYaml .Values.agentclusterrole.rules | indent 0 }}
{{- if .Values.ocp }}
- apiGroups:
  - security.openshift.io
  resourceNames:
  - privileged
  resources:
  - securitycontextconstraints
  verbs:
  - use
{{- end -}}

{{- end -}}

{{- define "manager-chart-library.agentclusterrolebinding" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ .Values.agentclusterrolebinding.name }}
subjects:
- kind: ServiceAccount
  name: {{ .Values.agentclusterrolebinding.serviceaccount.name }}
  namespace: {{ .Values.agentclusterrolebinding.serviceaccount.namespace }}
roleRef:
  kind: ClusterRole
  name: {{ .Values.agentclusterrolebinding.clusterrolename }}
  apiGroup: rbac.authorization.k8s.io

{{- end -}}
