# Cluster Power Manager GitOps policies

An ACM `PolicyGenerator` that installs Cluster Power Manager (CPM) with OLMv0,
labels the nodes where the Power Node Agent runs, creates the CPM custom
resources and verifies the per-node `PowerNodeState`. For what the CPM
resources mean, see the [main README](../README.md).

The layout follows the [telco-reference ACM PolicyGenerator
examples](https://github.com/openshift-kni/telco-reference/tree/main/telco-ran/configuration/acmpolicygenerator),
which also cover the ArgoCD plugin setup and how to patch lists in source CRs.
The `PolicyGenerator` is rendered by the [ACM policy-generator](https://github.com/stolostron/policy-generator-plugin) kustomize plugin; do not apply it to the hub directly.

## Layout

| Path | Purpose |
|---|---|
| `cpm.yaml` | The `PolicyGenerator`: seven policies sharing one `Placement` and `PlacementBinding`. |
| `kustomization.yaml` | Lists `cpm.yaml` as generator, and `ns.yaml` and `template-values/` as resources. |
| `ns.yaml` | The hub policy namespace (`ztp-cpm`) and its `ManagedClusterSetBinding`. |
| `reference-crs/` | The source CRs that `cpm.yaml` references. |
| `template-values/` | `cpm-hardware-types-configmap`: the per-hardware-class values read by the hub templates in `cpm.yaml`. |

## Prerequisites

- An ACM hub that supports `PolicyGenerator` (ACM 2.10 or later), with the
  policy-generator kustomize plugin installed in the GitOps repo server, as
  described in the [telco-reference ArgoCD setup](https://github.com/openshift-kni/telco-reference/tree/main/telco-ran/configuration/argocd).
- OpenShift managed clusters with OLMv0 and a catalog that provides the
  `cluster-power-manager` package. `reference-crs/CpmSubscription.yaml` defaults
  to the `redhat-operators-disconnected` catalog in `openshift-marketplace` and
  the `dev-preview-stable` channel; change `spec.source`, `spec.sourceNamespace`
  and `spec.channel` to match your catalog.
- Kubelet with the static CPU manager and a `reservedSystemCPUs` value that
  matches the `reservedCPUs` of the node's class in `cpm-hardware-types-configmap`
  (for example `cpuManagerPolicy: "static"` and `reservedSystemCPUs: "0,1"`).
- Any other CPU power-management component, such as the
  cluster-node-tuning-operator `[cpu]` and `[uncore]` plugins, configured not to
  conflict with CPM.
- Node Feature Discovery is optional. Nothing here depends on it.

## Select the managed clusters

The policies are placed on managed clusters labelled `cpm=true`:

```console
oc label managedcluster <cluster-name> cpm=true --overwrite
```

The cluster label only selects clusters. Which node gets which profile is
decided by node labels, described below. If your hub uses a different cluster
set or namespace, update `policyDefaults.placement`,
`policyDefaults.namespace` and `ns.yaml` together.

## Hardware classes and node labels

The tuning values come from `cpm-hardware-types-configmap`, which the hub
templates in `cpm.yaml` read with literal `<class>-<field>` keys. It must be
in the `ztp-cpm` namespace and contain every templated key.

A class is a group of nodes with the same CPU hardware, selected by the node
label `cpm-hardware-class=<class>`. In this example, each class has its own
`shared-<class>` and `performance-<class>` PowerProfiles and `shared-config-<class>` PowerNodeConfig.

ACM does not copy ManagedCluster labels to nodes, so set the node labels on the
nodes:

- `feature.node.kubernetes.io/power-node=true` is set by the `cpm-node-labels`
  policy on all workers, or on the control-plane node when there are none.
- `cpm-hardware-class=<class>` is not set by anything here. Set it at install
  time, through NFD, or with your own policy.

The PowerProfiles and PowerNodeConfig are created whether or not any node
matches their selector, and the policies stay compliant. The validator policy
reports a power node whose `cpm-hardware-class` is missing or is not a defined
class. Nodes without the power-node label are not checked. `reservedCPUs` is
per class, so clusters of one class cannot differ.

## Policies and order

All policies are `inform`; TALM enforces them in a `ClusterGroupUpgrade`.
Outside TALM, switch `remediationAction` to `enforce` and consider enabling
`orderPolicies` in `cpm.yaml`. TALM orders the policies by the
`ran.openshift.io/ztp-deploy-wave` annotation, set per policy with
`policyAnnotations`.

| Policy | Wave | Action |
|---|---|---|
| `cpm-olm-install` | 2 | Create the `power-manager` namespace, OperatorGroup and Subscription. |
| `cpm-olm-install-status` | 2 | Verify the OLM installation. |
| `cpm-node-labels` | 3 | Label the nodes that run the Power Node Agent. |
| `cpm-power-config` | 4 | Create the `PowerConfig` and start the node agent. |
| `cpm-power-profiles` | 5 | Create the shared and performance PowerProfiles of each class. |
| `cpm-power-node-config` | 6 | Create the PowerNodeConfig of each class. |
| `cpm-power-node-state-validator` | 10000 | Verify that each power node has a `PowerNodeState` whose status references its class's pools and profiles. |

The `power-manager` namespace is annotated `workload.openshift.io/allowed:
management`. The CPM pods themselves are not annotated, so on clusters with
workload partitioning they are not pinned to the reserved CPUs unless the
operator's own manifests add `target.workload.openshift.io/management`.

## Render and verify

Point the ArgoCD application at this directory, as in telco-reference. To render
locally with the plugin installed:

```console
kustomize build --enable-alpha-plugins gitops-ztp -o /tmp/cpm-policies
```

Check compliance on the hub and the node state on a managed cluster:

```console
oc get policy -n ztp-cpm
oc get pns <node-name>-power-state -n power-manager -o yaml
```

The validator only checks that the `PowerNodeState` exists and references the
class's pools and profiles. It does not check CPU IDs, profile config strings or
error lists; inspect those in the `PowerNodeState` status.
