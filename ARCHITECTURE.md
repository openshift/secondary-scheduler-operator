# Architecture

## Overview

Secondary Scheduler Operator is an OpenShift operator that deploys and manages a secondary Kubernetes scheduler alongside the default scheduler. It allows users to run a customized scheduler image (built using the [Kubernetes scheduler plugin framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)) as a secondary scheduler. Pods can opt in to the secondary scheduler by setting `spec.schedulerName: secondary-scheduler`.

The operator's primary responsibilities:
- Watch the `SecondaryScheduler` CR and reconcile the operand lifecycle
- Deploy and manage a secondary scheduler Deployment with the user-specified scheduler image and configuration
- Observe cluster-level TLS settings and inject them into the scheduler
- Support both single-replica and highly-available deployment topologies

## Data Flow

```text
  SecondaryScheduler CR (operator.openshift.io/v1)
  (name: cluster, namespace: openshift-secondary-scheduler-operator)
              │
              ▼
  ┌──────────────────────────────────────────────────────┐
  │        TargetConfigReconciler (pkg/operator)          │
  │  (watches CR + ConfigMap, reconciles all operand      │
  │   resources from embedded YAML templates)             │
  └──────────────────┬───────────────────────────────────┘
                     │
      ┌──────────────┼──────────────────┐
      ▼              ▼                  ▼
  Deployment    ServiceAccount     Monitoring
  (scheduler)   + RBAC             (Service,
                                    ServiceMonitor)
      │
      ▼
  Secondary Scheduler Pod(s)
  (runs /bin/kube-scheduler with custom config)
      │
      ▼
  Pods with schedulerName: secondary-scheduler
  are scheduled by this scheduler
```

Users create a `SecondaryScheduler` CR specifying a scheduler image and a ConfigMap containing a `KubeSchedulerConfiguration`. The operator deploys and manages the scheduler as a Deployment in the `openshift-secondary-scheduler-operator` namespace.

## Operator Startup

Entry point: `cmd/secondary-scheduler-operator/main.go` → `pkg/cmd/operator/cmd.go` → `pkg/operator/starter.go`.

Startup sequence:
1. Create clients (Kubernetes, dynamic, OpenShift config, OpenShift route)
2. Set up informers for the operator namespace and cluster-wide resources
3. Create a `SecondarySchedulerClient` (operator client adapter)
4. Start four controllers:
   - **ResourceSyncController** — syncs secrets/configmaps between namespaces
   - **ConfigObserver** — observes TLS security profile from the cluster `APIServer` config
   - **TargetConfigReconciler** — the main reconciliation loop
   - **LogLevelController** — adjusts log level based on operator CR
5. Block until context cancellation

The operator uses OpenShift's `library-go` `controllercmd` framework, which provides leader election, health checks, and graceful shutdown.

## Custom Resource

The `SecondaryScheduler` CRD (`operator.openshift.io/v1`) is namespaced and defines:

- **Spec fields** (embeds `operatorv1.OperatorSpec`):
  - `schedulerConfig` (string) — name of a ConfigMap containing `KubeSchedulerConfiguration` YAML
  - `schedulerImage` (string) — container image of the secondary scheduler
  - `topology` — deployment topology configuration:
    - `mode`: `SingleReplica` (default) or `HighlyAvailable`
    - `highlyAvailableTopology`: `nodeSelector`, `tolerations`, `maxReplicas` (min: 1, default: 3)
    - CEL validation: `highlyAvailableTopology` can only be set when `mode` is `HighlyAvailable`
  - Standard operator fields: `managementState`, `logLevel`, `unsupportedConfigOverrides`, `observedConfig`
- **Status fields** (embeds `operatorv1.OperatorStatus`): `conditions[]`, `generations[]`, `observedGeneration`

The CR must be named `cluster` and created in the `openshift-secondary-scheduler-operator` namespace.

## TargetConfigReconciler

`pkg/operator/target_config_reconciler.go` is the central reconciler. On each sync it:

1. Fetches the `SecondaryScheduler` CR named `cluster`
2. Manages operand resources using embedded YAML templates from `bindata/`:
   - ServiceAccount (`secondary-scheduler`)
   - Two ClusterRoleBindings (kube-scheduler and volume-scheduler roles)
   - Service (metrics endpoint)
   - Role and RoleBinding (Prometheus access)
   - Operand Role and RoleBinding (scheduler's own needs)
   - ServiceMonitor (Prometheus monitoring)
   - Deployment (the secondary scheduler itself)
3. For the Deployment, performs template substitution:
   - `${IMAGE}` → `spec.schedulerImage`
   - `${CONFIGMAP}` → `spec.schedulerConfig`
   - Log verbosity from `spec.logLevel` (Normal=2, Debug=4, Trace=6, TraceAll=8)
   - TLS cipher suites and min TLS version from `observedConfig`
4. In **HighlyAvailable** mode: counts matching nodes, sets replicas (capped by `maxReplicas`), applies nodeSelector and tolerations
5. Sets OwnerReferences on all managed resources pointing to the CR (see Design Decisions for garbage collection implications)
6. Tracks resource versions as annotations on the Deployment pod template to trigger rolling updates on ConfigMap or other resource changes

## Config Observer

`pkg/operator/configobservation/` observes the cluster-level `APIServer` configuration for TLS security profile settings. When the cluster admin changes the TLS profile, the observer updates the operator's `observedConfig`, which triggers the reconciler to update the scheduler Deployment args with the new TLS cipher suites and minimum TLS version.

## Embedded Assets

`bindata/assets.go` uses Go 1.16+ `//go:embed assets/*` to embed YAML templates from `bindata/assets/secondary-scheduler/`:

| Asset | Purpose |
|-------|---------|
| `deployment.yaml` | Scheduler Deployment template with `${IMAGE}` and `${CONFIGMAP}` placeholders |
| `serviceaccount.yaml` | ServiceAccount for the scheduler |
| `clusterrolebinding-system-kube-scheduler.yaml` | Binds `system:kube-scheduler` ClusterRole |
| `clusterrolebinding-system-volume-scheduler.yaml` | Binds `system:volume-scheduler` ClusterRole |
| `service.yaml` | Metrics Service |
| `role.yaml`, `rolebinding.yaml` | Prometheus RBAC |
| `operandrole.yaml`, `operandrolebinding.yaml` | Scheduler namespace RBAC |
| `servicemonitor.yaml` | Prometheus ServiceMonitor |

The Deployment template runs `/bin/kube-scheduler` with `--config`, TLS cert/key, and leader election args, using the `restricted-v2` SCC.

## Testing

**Unit tests**: Co-located `*_test.go` files in `pkg/operator/`. Comprehensive coverage of the reconciler including deployment creation, idempotency, drift correction, image/configmap substitution, log level mapping, TLS configuration, topology/HA mode, resource lifecycle, and owner references.

**E2E tests** (`test/e2e/`): Deployed via the OpenShift Tests Extension (OTE) framework. Tests include:
- Pod scheduling via the secondary scheduler (creates a pod with `schedulerName: secondary-scheduler` and verifies it gets scheduled)
- Observability: metrics Service exists, ServiceMonitor exists, Prometheus target is up, metrics data is available
- HA mode toggle: SingleReplica → HighlyAvailable (verifies 3 replicas, pod anti-affinity) → SingleReplica (verifies scale-down to 1)

The OTE test binary is built alongside the operator and shipped (gzipped) in the operator image at `/usr/bin/secondary-scheduler-operator-tests-ext.gz`.

## Namespace

Everything runs in `openshift-secondary-scheduler-operator` (constant `OperatorNamespace` in `pkg/operator/operatorclient/`). The operator Deployment, operand Deployment, RBAC, Service, and ServiceMonitor all live here.

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| `library-go` controller framework | Consistent with other OpenShift operators; provides battle-tested leader election, health checks, config observation |
| Embedded YAML templates with placeholder substitution | Simple, auditable resource definitions; avoids programmatic resource construction |
| ConfigMap-based scheduler configuration | Users bring their own `KubeSchedulerConfiguration`; decouples scheduler config from operator config |
| Resource version annotations on pod template | Forces rolling update when ConfigMap or other dependencies change, even if the Deployment spec itself hasn't |
| OwnerReferences on namespaced managed resources | Automatic garbage collection of ServiceAccount, Service, Roles, RoleBindings, Deployment, ServiceMonitor when the CR is deleted; ClusterRoleBindings cannot rely on OwnerReferences for garbage collection since they are cluster-scoped |
| HA mode with node counting | Replicas scale to available matching nodes (capped by `maxReplicas`); avoids over-provisioning |
| OTE test binary shipped in operator image | Enables CI to extract and run e2e tests without a separate test image |
