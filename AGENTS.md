# AI Agent Instructions for secondary-scheduler-operator

## What This Repo Is

This is an OpenShift operator that deploys custom Kubernetes schedulers built with the [scheduler plugin framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/). It allows users to run additional schedulers alongside the default OpenShift scheduler with customized scheduling policies and plugin configurations.

## Repository Structure

```
secondary-scheduler-operator/
├── cmd/
│   ├── secondary-scheduler-operator/           # Main operator binary
│   └── secondary-scheduler-operator-tests-ext/ # OTE test harness
├── pkg/
│   ├── operator/                               # Core operator logic
│   │   ├── starter.go                          # Controller initialization
│   │   ├── target_config_reconciler.go         # Main reconciliation controller
│   │   ├── configobservation/                  # TLS config observer
│   │   └── operatorclient/                     # Operator client wrapper
│   ├── apis/secondaryscheduler/v1/             # API type definitions
│   ├── generated/                              # Generated clientsets, informers, listers
│   ├── cmd/operator/                           # Operator command setup
│   ├── version/                                # Version information
│   └── dependencymagnet/                       # Dependency imports for vendoring
├── test/
│   └── e2e/                                    # E2E test suites (serial and parallel)
├── bindata/                                    # Embedded YAML assets for operand resources
│   └── assets/secondary-scheduler/             # ServiceAccount, ClusterRoleBindings, Deployment, etc.
├── deploy/                                     # Sample deployment manifests
├── manifests/                                  # CRD and CSV definitions
├── hack/                                       # Build and codegen scripts
└── vendor/                                     # Go dependencies (managed by go mod)
```

## Build/Verify Workflow

```bash
make build               # Compile operator + test binaries (outputs to ./)
make test-unit           # Unit tests (./pkg/... ./cmd/...)
make test-e2e            # E2E tests via OTE harness (requires OpenShift cluster)
                         # - Serial suite: openshift/secondary-scheduler-operator/operator/serial
make verify              # Run all verification checks (gofmt, etc.)
make generate            # Regenerate CRDs and clients (runs update-codegen-crds and generate-clients)
make install-local       # Apply CRD, namespace, configmap, and CR to cluster
make run-local           # Run operator locally against KUBECONFIG cluster
```

## Critical Rules

1. **Do not edit generated files.** Files matching `zz_generated.*` under `pkg/generated/` and `pkg/apis/` are code-generated. Run `make generate-clients` to regenerate them.
2. **Do not edit vendored files.** The `vendor/` directory is managed by `go mod tidy && go mod vendor`. Never hand-edit anything under `vendor/`.
3. **CRD is owned by this repo.** The `SecondaryScheduler` CRD is defined here in `manifests/secondary-scheduler-operator.crd.yaml`. The types in `pkg/apis/secondaryscheduler/v1/types.go` embed `operatorv1.OperatorSpec/OperatorStatus` from `openshift/api`.
4. **Bindata assets are embedded.** The YAML files in `bindata/assets/secondary-scheduler/` are embedded into the operator binary at build time. Changes require rebuilding the operator.
5. **Use make build, not go build.** The Makefile uses `build-machinery-go` targets that handle codegen and vendoring correctly.

## Key Patterns

- **Single CR reconciliation**: The operator watches the `SecondaryScheduler` CR named `cluster` in the `openshift-secondary-scheduler-operator` namespace. All configuration comes from this singleton CR.
- **Resource ownership**: All operand resources (Deployment, ServiceAccount, ClusterRoleBindings, Service, ServiceMonitor) are created with `OwnerReference` pointing to the SecondaryScheduler CR for garbage collection.
- **Annotation-driven rollouts**: The Deployment's pod template is annotated with resource versions of ConfigMaps and other dependencies to trigger automatic rollouts when configuration changes.
- **HA mode with dynamic replicas**: When `topology.mode=HighlyAvailable`, the operator counts nodes matching the `nodeSelector` and sets replicas accordingly, capped at `maxReplicas` (default 3). Only **one replica actively schedules** at a time due to leader election — the others are standby for high availability failover.

## Important Constraints

- **Namespace is fixed**: The operator expects the CR to exist in `openshift-secondary-scheduler-operator` namespace with name `cluster` (see `pkg/operator/operatorclient/interfaces.go:24-26`).
- **ConfigMap must exist**: The CR's `spec.schedulerConfig` field must reference an existing ConfigMap containing a valid `KubeSchedulerConfiguration` under the key `config.yaml`.
- **Leader election is enabled**: The scheduler runs with `--leader-elect=true`, so in HA mode only one replica actively schedules pods at a time. The other replicas are standby and take over if the leader fails.
- **Pod anti-affinity is soft**: In HA mode, the operator configures `preferredDuringSchedulingIgnoredDuringExecution` pod anti-affinity to spread replicas across nodes, but it's not a hard requirement.

## What NOT to Do

- **Do not modify OWNERS or OWNERS_ALIASES files** without authorization.
- **Do not change the operator namespace** (`openshift-secondary-scheduler-operator`) — it's hardcoded in many places and expected by the operand.
- **Do not skip `make verify`** — CI will fail if linters or formatting checks fail.
- **Do not run E2E tests on non-OpenShift clusters** — the tests expect OpenShift-specific resources (Routes for Prometheus access) and Prometheus Operator (ServiceMonitor CRD).
- **Do not modify the scheduler image** — the operator deploys whatever image is specified in `spec.schedulerImage`. The operator itself doesn't build or manage scheduler images.
