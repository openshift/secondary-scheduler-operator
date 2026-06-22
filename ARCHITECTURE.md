# Architecture Overview

## Scope

This operator manages **custom secondary schedulers** for OpenShift/Kubernetes clusters. It reconciles the `SecondaryScheduler` CR and deploys a customized scheduler image (built with the scheduler plugin framework) alongside OpenShift's default scheduler. The operator handles RBAC, configuration mounting, High Availability topology, and observability (metrics via ServiceMonitor). TLS certificates for the metrics endpoint are automatically provisioned by OpenShift's service-ca-operator.

## Controllers

The operator runs **4 concurrent controllers** (all sharing informers and event handlers):

1. **TargetConfigReconciler** (`pkg/operator/target_config_reconciler.go`): Main reconciliation loop that manages the scheduler Deployment and all supporting resources (ServiceAccount, ClusterRoleBindings, Service, Role, RoleBinding, ServiceMonitor).
2. **ConfigObserver** (`pkg/operator/configobservation/configobservercontroller/`): Watches the cluster `APIServer` config for TLS settings (cipher suites, minTLSVersion) and syncs them to the SecondaryScheduler CR's `observedConfig`.
3. **ResourceSyncController** (`library-go`): Syncs ConfigMaps and Secrets between namespaces if needed.
4. **LogLevelController** (`library-go`): Adjusts operator logging based on the CR's `logLevel` field.

All controllers use **informers** from `library-go`'s `KubeInformersForNamespaces` to watch resources efficiently.

## Reconciliation Flow

```text
Event (SecondaryScheduler CR, ConfigMap, Deployment, etc.)
  → Enqueue queueItem{kind: "secondaryscheduler"} or {kind: "configmap", name: "..."}
    → sync(item)
      ├── Get SecondaryScheduler CR (cluster/openshift-secondary-scheduler-operator)
      ├── Skip reconciliation if item is ConfigMap and doesn't match schedulerConfig
      ├── Collect resource versions for annotations:
      │   ├── ConfigMap/<schedulerConfig> ResourceVersion
      │   ├── Managed resources (ServiceAccount, ClusterRoleBindings, Service, etc.)
      │   └── Store in specAnnotations map for Deployment pod template
      ├── Resource management (in order):
      │   ├── manageServiceAccount()
      │   ├── manageKubeSchedulerClusterRoleBinding()
      │   ├── manageVolumeSchedulerClusterRoleBinding()
      │   ├── manageService()
      │   ├── manageRole() / manageRoleBinding() (for prometheus access)
      │   ├── manageOperandRole() / manageOperandRoleBinding()
      │   ├── manageServiceMonitor()
      │   └── manageDeployment() — applies all annotations and HA topology config
      └── Update SecondaryScheduler status with Deployment generation
```

## Resource Generation

The operator uses **embedded YAML templates** from `bindata/assets/secondary-scheduler/`:

- **ServiceAccount** (`serviceaccount.yaml`): `secondary-scheduler` SA for the scheduler pods
- **ClusterRoleBindings** (`clusterrolebinding-*.yaml`): Bind to `system:kube-scheduler` and `system:volume-scheduler` ClusterRoles
- **Deployment** (`deployment.yaml`): Scheduler pod spec with placeholders `${IMAGE}` and `${CONFIGMAP}`
- **Service** (`service.yaml`): Metrics service exposing port 10259
- **ServiceMonitor** (`servicemonitor.yaml`): Prometheus scraping configuration
- **Role/RoleBinding** (`role.yaml`, `rolebinding.yaml`): Grant prometheus access to scrape metrics
- **OperandRole/OperandRoleBinding** (`operandrole.yaml`, `operandrolebinding.yaml`): Additional RBAC for the operand

Each resource is loaded via `resourceread.*OrDie()`, modified in-memory (image substitution, ConfigMap name, OwnerReferences, etc.), and applied using `resourceapply.Apply*()` from `library-go`.

### Deployment Customization

The `manageDeployment()` function performs several key transformations:

1. **Image substitution**: Replaces `${IMAGE}` with `spec.schedulerImage`
2. **ConfigMap mounting**: Replaces `${CONFIGMAP}` volume source with `spec.schedulerConfig`
3. **Log level args**: Appends `-v=<level>` based on `spec.logLevel` (Normal=2, Debug=4, Trace=6, TraceAll=8)
4. **TLS configuration**: Parses `spec.observedConfig.Raw` for `servingInfo.cipherSuites` and `minTLSVersion`, appends as scheduler args
5. **HA topology**:
   - If `topology.mode=HighlyAvailable`:
     - Count nodes matching `highlyAvailableTopology.nodeSelector`
     - Set replicas to min(node_count, maxReplicas)
     - Apply `nodeSelector` and `tolerations` to pod template
6. **Annotation merging**: Merge `specAnnotations` (resource versions) into pod template to force rollouts on config changes

## CRDs Reconciled

- **`SecondaryScheduler`** (`secondaryschedulers.operator.openshift.io/v1`): Main CR defining scheduler configuration
  - `spec.schedulerConfig`: ConfigMap name containing `KubeSchedulerConfiguration`
  - `spec.schedulerImage`: Container image for the custom scheduler
  - `spec.topology`: HA configuration (mode, nodeSelector, tolerations, maxReplicas)
  - `spec.observedConfig`: TLS settings synced from cluster APIServer config
  - `spec.logLevel`: Operator log verbosity (Normal, Debug, Trace, TraceAll)

## High Availability Mode

When `topology.mode: HighlyAvailable`:

1. **Dynamic replica calculation**:
   ```go
   nodes := ListNodes(labelSelector: highlyAvailableTopology.nodeSelector)
   replicas := min(len(nodes), maxReplicas)  // maxReplicas defaults to 3
   ```

2. **Pod anti-affinity** (soft, weight 100):
   ```yaml
   preferredDuringSchedulingIgnoredDuringExecution:
     - weight: 100
       podAffinityTerm:
         labelSelector:
           matchLabels:
             app: secondary-scheduler
         topologyKey: kubernetes.io/hostname
   ```
   This spreads replicas across nodes but allows multiple on the same node if needed.

3. **Leader election**: The scheduler runs with `--leader-elect=true`, so only one replica actively schedules at a time. Other replicas are **standby** — they don't schedule pods, but are ready to take over if the active leader fails or becomes unreachable. This provides high availability without duplicate scheduling work.

## Dependencies

```text
secondary-scheduler-operator
├── depends on
│   ├── openshift/api                (SecondaryScheduler CRD, APIServer, Infrastructure)
│   ├── openshift/client-go          (generated clients for OpenShift APIs)
│   ├── openshift/library-go         (controller framework, resourceapply, status helpers)
│   ├── k8s.io/client-go             (informers, work queues, retry logic)
│   └── prometheus-operator/apis     (ServiceMonitor CRD)
│
├── manages
│   ├── Deployment                   (openshift-secondary-scheduler-operator/secondary-scheduler)
│   ├── ServiceAccount + RBAC        (ClusterRoleBindings, Role, RoleBinding)
│   ├── Service (metrics)
│   ├── ServiceMonitor               (Prometheus scraping)
│   └── Secret                       (TLS certificates via service-ca)
│
└── reads configuration from
    ├── SecondaryScheduler CR        (cluster/openshift-secondary-scheduler-operator)
    ├── ConfigMap                    (user-provided KubeSchedulerConfiguration)
    └── APIServer CR                 (cluster TLS settings via configObserver)
```

## Design Decisions

### Why single CR reconciliation?

The operator expects exactly one CR named `cluster` in namespace `openshift-secondary-scheduler-operator`. This singleton pattern simplifies reconciliation logic and status tracking — there's no need to handle multiple scheduler deployments or namespaces.

### Why embed YAML assets instead of generating programmatically?

Embedding YAML templates makes it easier to review and version-control the exact manifests deployed by the operator. The templates use simple placeholder substitution (`${IMAGE}`, `${CONFIGMAP}`) rather than complex Go structs.

### Why soft pod anti-affinity instead of hard?

Hard anti-affinity (`requiredDuringSchedulingIgnoredDuringExecution`) would prevent scheduling if there aren't enough nodes. Soft anti-affinity allows the scheduler to run in constrained environments (single-node, or fewer nodes than replicas) while still spreading pods when possible.

### Why annotation-driven rollouts?

Embedding resource versions of ConfigMaps and other dependencies in the Deployment's pod template annotations forces Kubernetes to perform a rolling update whenever configuration changes. This ensures the scheduler always runs with the latest config without manual restarts.

### Why ConfigObserver for TLS settings?

The cluster APIServer CR defines global TLS policy. By observing and syncing these settings to the SecondaryScheduler CR's `observedConfig`, the operator ensures the scheduler respects cluster-wide security policies without requiring manual configuration.

## Testing

- **Unit tests**: `pkg/operator/*_test.go` (reconciler logic, HA replica calculation)
- **E2E tests**: `test/e2e/` (full operator lifecycle on OpenShift clusters)
  - **Serial suite** (`openshift/secondary-scheduler-operator/operator/serial`): Operator deployment, scheduling, HA mode, observability
    - Tests run with `-c 1` (serial) to avoid conflicts
    - Uses OTE framework ([OpenShift Tests Extension](https://github.com/openshift-eng/openshift-tests-extension))
  - **Framework**: `test/e2e/helpers.go` provides client constructors, Prometheus query helpers, etc.

### E2E Test Flow

1. `setupOperator()`: Apply CRD, namespace, RBAC, operator Deployment, ConfigMap, and SecondaryScheduler CR
2. Wait for operator pod to be Running
3. Test scheduling: Create a pod with `schedulerName: secondary-scheduler`, verify it gets assigned to a node
4. Test observability: Verify Service, ServiceMonitor exist and Prometheus scrapes metrics
5. Test HA mode: Verify replica count matches node count (capped at maxReplicas), verify anti-affinity

## Observability

- **Metrics**: Exposed on port 10259 (HTTPS) via the scheduler's built-in metrics endpoint
- **ServiceMonitor**: Configures Prometheus to scrape `scheduler_*` metrics from the secondary-scheduler pods
- **Prometheus RBAC**: The operator creates a Role/RoleBinding to allow prometheus-k8s ServiceAccount to read endpoints/services/pods in the operator namespace

## Configuration Observer

The `ConfigObserver` controller watches:

- **APIServer CR** (`config.openshift.io/v1`): Global cluster API server configuration
  - Extracts `spec.tlsSecurityProfile.cipherSuites` and `spec.tlsSecurityProfile.minTLSVersion`
  - Writes them to `SecondaryScheduler.spec.observedConfig.servingInfo` as unstructured JSON

The `TargetConfigReconciler` then reads these values and appends them as scheduler command-line args:
```bash
--tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,...
--tls-min-version=VersionTLS12
```
