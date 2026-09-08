---
title: KPM Architecture Redesign for Multi-Node Support
authors:
  - "@Missxiaoguo"
reviewers:
  - "@bartwensley"
  - "@browsell"
  - "@irinamihai"
approvers:
  - "@bartwensley"
creation-date: 2026-02-24
last-updated: YYYY-MM-DD
---

# KPM Architecture Redesign for Multi-Node Support

## Summary

Redesign the Kubernetes Power Manager (KPM) Custom Resource (CR) and controller architecture to properly support multi-node clusters.
This enhancement proposes:

- Introducing a new `PowerNodeConfig` CR for shared/reserved CPU configuration
  - Multi-node targeting is done via `nodeSelector`
- Adding `nodeSelector` to the `Uncore` CR for multi-node targeting
- Renaming `PowerNode` to `PowerNodeState` as the single per-node status CR
- Removing the `PowerWorkload` CR, and restructuring controller responsibilities so each agent writes only to its own `PowerNodeState` fields

## Motivation

### Current Problems

1. **Multi-agent write conflicts on shared CRs**: When a `PowerProfile` targets multiple nodes via `nodeSelector`, all matching node agents write to the same `PowerProfile.status.errors[]` field. This causes resource version conflicts, incomplete error reporting and log spam.

2. **No per-node error visibility**: `PowerProfile.status.errors[]` is a flat array with no indication of which node reported the error. In heterogeneous clusters where some nodes fail validation and others succeed, the user has no way to determine the source of errors.

3. **Per-node CRs for identical configuration**: Configuring shared and reserved CPU power settings requires creating a separate `PowerWorkload` CR per node,
even when the configuration is identical across nodes of the same type. For a 100-node cluster with identical worker nodes, this means 100 identical `PowerWorkload` CRs.
This is operationally burdensome and does not scale automatically with GitOps workflows -- every time a new node joins the cluster, a new CR must be committed to the GitOps repo for that node. The same applies to `Uncore` CRs.

### Goals

- Eliminate multi-agent write conflicts on shared CRs in multi-node clusters
- Provide per-node error visibility for heterogeneous environments
- Simplify user experience by reducing the number of CRs needed to configure identical nodes
- Scale with GitOps workflows without requiring per-node CR commits
- Consolidate redundant controller logic

### User Stories

1. As a cluster administrator, I want to create a single CR that configures shared CPU power settings for all my identical worker nodes, so I don't have to duplicate configuration per node.

2. As a cluster administrator using GitOps, I want to define power management configuration once in my repo and have new nodes automatically pick it up via label selectors, without committing a new CR for each node.

3. As a cluster administrator with heterogeneous hardware, I want to see per-node validation errors in a single place, so I can quickly identify which nodes have issues without checking each node's agent logs.

## Current Architecture

![KPM Current Architecture](kpm-current-state.png)

### Custom Resources

| Custom Resource             | Created By                    | Purpose                                                          |
|-----------------------------|-------------------------------|------------------------------------------------------------------|
| PowerConfig                 | User                          | Configure operator deployment and node selection                 |
| PowerProfile                | User                          | Define power settings (P-states, C-states, scaling policy)       |
| PowerWorkload (exclusive)   | System (PowerProfile ctrl)    | Per-node exclusive CPU workload, auto-created per profile        |
| PowerWorkload (shared)      | User                          | Per-node shared/reserved CPU configuration                       |
| PowerNode                   | System (PowerConfig ctrl)     | Per-node status aggregation                                      |
| Uncore                      | User                          | Per-node uncore frequency settings                               |

### Controllers

**Central Manager (Deployment):**

| Controller    | Responsibilities                                                | Writes To                      |
|---------------|-----------------------------------------------------------------|--------------------------------|
| PowerConfig   | Reconciles PowerConfig. Creates PowerNode and DaemonSet.        | PowerNode.status.customDevices |

**Node Agent (DaemonSet, per node):**

| Controller    | Responsibilities                                                                                                                                | Writes To                                |
|---------------|-------------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------|
| PowerProfile  | Validates profile against node. Creates internal Power Optimization Library (POL) profile/pool, exclusive PowerWorkload and extended resources. | PowerProfile.status                      |
| PowerWorkload | Applies power config for shared, reserved and exclusive CPUs via POL.                                                                           | PowerWorkload.status (name)              |
| PowerPod      | Watches Pods. Detects exclusive CPU assignments.                                                                                                | PowerWorkload.status (cpuIds, containers)|
| PowerNode     | Re-queries POL state and aggregates status.                                                                                                     | PowerNode.status                         |
| Uncore        | Validates and applies uncore frequency configuration.                                                                                           | Uncore.status                            |

## Proposal

### Design Principles

1. Avoid multi-agent writes to the same CR status: Each node agent writes per-node status exclusively to its own CR, preventing resource version conflicts
2. User-facing CRs are cluster-wide with nodeSelector: Users create one CR and it applies to all matching nodes

### Proposed Architecture

![KPM Proposed Architecture](kpm-proposal.png)

*Note: The fields shown in the diagram are illustrative. The exact field structure and naming in PowerNodeConfig and PowerNodeState may be finalized during implementation.*

### Custom Resource Changes

| Custom Resource                 | Change                                  | Created By                | Purpose                                                                                            |
|---------------------------------|-----------------------------------------|---------------------------|----------------------------------------------------------------------------------------------------|
| PowerConfig                     | Unchanged                               | User                      | Configure operator deployment and node selection                                                   |
| PowerProfile                    | Per-node status moved to PowerNodeState | User                      | Define power settings (P-states, C-states, scaling policy)                                         |
| **PowerNodeConfig** (new)       | Replaces shared PowerWorkload           | User                      | Configure shared/reserved CPU power settings for matching nodes via `nodeSelector`                 |
| **PowerNodeState** (renamed)    | Replaces PowerNode, expanded status     | System (PowerConfig ctrl) | Per-node status: profile validation, CPU pool configuration, uncore settings, and error reporting  |
| Uncore                          | Added `nodeSelector`                    | User                      | Uncore frequency settings for matching nodes via `nodeSelector`                                    |
| PowerWorkload                   | **Removed**                             | -                         | Replaced by PowerNodeConfig (shared/reserved) and PowerPod (exclusive)                             |

### Controller Changes

**Central Manager (Deployment):**

| Controller                   | Change    | Responsibilities                                                                                                    | Writes To                           |
|------------------------------|-----------|---------------------------------------------------------------------------------------------------------------------|-------------------------------------|
| PowerConfig                  | Unchanged | Reconciles PowerConfig. Creates PowerNodeState and DaemonSet.                                                       | PowerNodeState.status.customDevices |
| Status Aggregator (optional) | **New**   | Future extension: aggregates per-node PowerNodeState into user-facing CRs (PowerProfile, PowerNodeConfig, Uncore).  | User-facing CRs                     |

**Node Agent (DaemonSet, per node):**

Each agent controller owns distinct fields in `PowerNodeState` using Server-Side Apply (SSA), so multiple controllers can safely write to the same `PowerNodeState` CR without conflicts.

| Controller                | Change                                                                                 | Responsibilities                                                                                                | Writes To (SSA field ownership)                        |
|---------------------------|----------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------|--------------------------------------------------------|
| PowerProfile              | No longer creates exclusive PowerWorkload. Writes to PowerNodeState.                   | Validates profile against node capabilities. Creates internal POL profile/pool and extended resources.          | PowerNodeState.powerProfiles                           |
| **PowerNodeConfig** (new) | Replaces shared PowerWorkload controller. Writes to PowerNodeState.                    | Applies power config to shared and reserved CPUs via POL.                                                       | PowerNodeState.cpuPools (shared, reserved, unaffected) |
| PowerPod                  | Now applies power config and DPDK scaling to exclusive CPUs. Writes to PowerNodeState. | Watches Pods. Detects exclusive CPU assignments. Applies power config and DPDK dynamic scaling.                 | PowerNodeState.cpuPools.exclusive                      |
| Uncore                    | Writes to PowerNodeState instead of Uncore.status.                                     | Validates and applies uncore frequency configuration.                                                           | PowerNodeState.uncore                                  |
| PowerNode                 | **Removed**                                                                            | Functionality absorbed into above controllers. Each writes status directly to PowerNodeState after processing.  | -                                                      |
| PowerWorkload             | **Removed**                                                                            | Shared workload functionality replaced by PowerNodeConfig controller. Exclusive replaced by PowerPod controller.| -                                                      |

### Key Design Decisions

**Why remove PowerWorkload CR entirely?**

- The `PowerWorkload` CR has inconsistent semantics: exclusive workloads are system-managed internal CRs auto-created by the `PowerProfile` controller, while shared workloads are user-facing CRs. This makes the API confusing.
- Shared `PowerWorkload` required one CR per node for identical configuration, creating operational burden at scale. `PowerNodeConfig` replaces this with a single cluster-wide CR using `nodeSelector`.
- Exclusive `PowerWorkload` CRs were system-managed intermediaries between the `PowerProfile` controller and the `PowerWorkload` controller.
The `PowerPod` controller already detects exclusive CPU assignments via the kubelet pod resources API and can apply profiles directly, making the intermediary CR unnecessary.

**Why remove PowerNode controller?**

The `PowerNode` controller re-queries POL state that the PowerProfile, PowerNodeConfig, and PowerPod controllers already have at the time they finish processing.
It only sees successes (active pools), not validation failures. Since these controllers already have all the necessary data at the point they complete their work,
they can write status directly to `PowerNodeState` after each successful or failed operation, rather than relying on a separate controller that runs later and rebuilds the same information.

### Admission Webhook

A node should only be selected by one `PowerNodeConfig` CR and one `Uncore` CR at a time to prevent conflicting power settings on the same node. Add a validating webhook to reject `PowerNodeConfig` or `Uncore` CRs when another CR of the same kind already selects the same set of nodes (matching `nodeSelector`).

## Alternatives

### Centralized Controller Generates Per-Node Shared PowerWorkload CRs

Instead of removing the `PowerWorkload` CR, introduce a new user-facing CR with `nodeSelector` for shared configuration and have a centralized controller translate it into per-node shared
`PowerWorkload` CRs with `ownerReferences`. The `PowerProfile` controller remains on the agent and continues to auto-create per-node exclusive `PowerWorkload` CRs. `Uncore` gains a
`nodeSelector` and `PowerNodeState` replaces `PowerNode` for per-node status, same as the selected proposal.

This approach provides centralized validation (profile existence, selector conflicts) done once and explicit per-node lifecycle management via `ownerReferences`. However, it has several
drawbacks. First, it results in CR sprawl -- the centralized controller creates N per-node shared `PowerWorkload` CRs for each user-facing config, and the `PowerProfile` agent controller
continues to create per-node exclusive `PowerWorkload` CRs. Second, this split means per-node CRs are created by both a centralized controller and an agent controller, leading to mixed ownership
and inconsistent cleanup patterns. Third, an aggregator controller is still needed to roll up per-node status from `PowerNodeState` into user-facing CRs, adding yet another controller to the
system.

### Why the Selected Proposal

The selected proposal results in the simplest architecture with the fewest CRs -- no per-node CRs beyond `PowerNodeState`. Each agent controller is self-contained: it reconciles cluster-wide
CRs matching its node, applies configuration locally, and writes results to its own `PowerNodeState`. This eliminates multi-writer conflicts and removes CR sprawl.
