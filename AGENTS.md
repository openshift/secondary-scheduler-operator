# AI Agent Guide for Secondary Scheduler Operator

This file provides guidance for AI agents working with the OpenShift Secondary Scheduler Operator repository.

## Overview

**What is Secondary Scheduler Operator?**
An OpenShift operator that deploys and manages a secondary Kubernetes scheduler alongside the default scheduler. It allows users to run a customized scheduler image (built using the [Kubernetes scheduler plugin framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)) as a secondary scheduler. Pods can opt in by setting `spec.schedulerName: secondary-scheduler`.

The operator is installed via the Operator Lifecycle Manager (OLM) and reconciles a `SecondaryScheduler` CR to deploy and manage the scheduler operand. It uses OpenShift's `library-go` controller framework (not `controller-runtime` or `operator-sdk`).

## Build and Test

```bash
make build        # Build all binaries
make test-unit    # Unit tests (pkg/... cmd/...)
make verify       # Formatting, vetting, golang version checks
make test-e2e     # E2E tests (requires cluster)
make generate     # Run all codegen (CRD schema + client generation)
make regen-crd    # Regenerate CRD from Go types using controller-gen
make sync-rbac    # Sync RBAC from CSV to test assets
```

Go version: see `go.mod`.

## Project Structure

| Directory / File | Purpose |
|-----------------|---------|
| `cmd/secondary-scheduler-operator/` | Main operator binary entry point |
| `cmd/secondary-scheduler-operator-tests-ext/` | OpenShift Tests Extension (OTE) binary for e2e tests |
| `pkg/apis/secondaryscheduler/v1/` | `SecondaryScheduler` CRD type definitions (`types.go`, `register.go`) |
| `pkg/cmd/operator/cmd.go` | Cobra command factory, wires `controllercmd` framework |
| `pkg/operator/starter.go` | `RunOperator()` — creates clients, informers, starts all controllers |
| `pkg/operator/target_config_reconciler.go` | Main reconciliation loop — watches CR + ConfigMap, manages all operand resources |
| `pkg/operator/operatorclient/interfaces.go` | Operator client adapter — constants (`OperatorNamespace`, `OperandName`), Get/Update/Apply |
| `pkg/operator/configobservation/` | TLS config observer — watches cluster `APIServer` config for TLS settings |
| `pkg/generated/` | Auto-generated clientset, informers, listers — **do not modify directly** |
| `pkg/version/version.go` | Build version info and Prometheus metric |
| `bindata/` | Embedded YAML templates for operand resources (Deployment, RBAC, Service, etc.) |
| `deploy/` | Manual (non-OLM) deployment manifests (numbered `00_` through `07_`) |
| `manifests/` | OLM bundle manifests (CSV, CRD, ClusterRoleBindings) |
| `metadata/` | OLM metadata annotations |
| `examples/` | Example `KubeSchedulerConfiguration` files |
| `test/e2e/` | E2E test suite — scheduling, observability, HA mode tests |
| `hack/` | Code generation and helper scripts |
| `vendor/` | Vendored dependencies — **do not modify directly** |
| `.tekton/` | Tekton/Konflux CI pipeline definitions |
| `Dockerfile` | Upstream container image build |
| `Dockerfile.rhel7` | CI container image build (uses RHEL 9 base despite the name) |
| `Makefile` | Build targets |
| `go.mod` | Go module dependencies |

## Controller Pattern

The operator runs four controllers, all wired in `pkg/operator/starter.go` via the library-go controller framework:

**`TargetConfigReconciler`** — the main reconciliation loop. Watches `SecondaryScheduler` CR and ConfigMap changes, reconciles all operand resources from embedded YAML templates:

```
fetch CR → manage RBAC → manage Service/ServiceMonitor → manage Deployment (with template substitution)
```

**`ConfigObserver`** — watches cluster `APIServer` config and observes TLS security profile changes, updating `observedConfig` on the CR.

**`ResourceSyncController`** — syncs secrets/configmaps between namespaces.

**`LogLevelController`** — adjusts operator log level based on the CR's `logLevel` field.

**Key Concepts:**
- **Informers:** Watch for CR, ConfigMap, Deployment, and cluster-level config changes
- **Template substitution:** `${IMAGE}` and `${CONFIGMAP}` placeholders in embedded YAML are replaced at reconcile time
- **Resource version annotations:** Tracked on the Deployment pod template to trigger rolling updates when dependencies change
- **OwnerReferences:** Set on all managed resources for automatic garbage collection

## Key Conventions

- **Namespace:** The operator and operand both run in `openshift-secondary-scheduler-operator`. Constants live in `pkg/operator/operatorclient/interfaces.go`.
- **CR name:** Must be `cluster` (constant `OperatorConfigName`).
- **Operand name:** `secondary-scheduler` (constant `OperandName`).
- **Logging:** `k8s.io/klog/v2` with verbosity levels mapped from `logLevel`: Normal=2, Debug=4, Trace=6, TraceAll=8.
- **Error handling:** Wrap with `fmt.Errorf("context: %w", err)`; return errors for retry, return `nil` for non-retriable conditions.
- **CRD changes:** Modify `pkg/apis/secondaryscheduler/v1/types.go`, then run `make regen-crd` and `make generate-clients`.
- **Build tags:** `strictfipsruntime` is set for all Go builds.

## Critical Rules

### DO NOT
1. **Don't modify CRD definitions** in `pkg/apis/secondaryscheduler/v1/` without understanding backward compatibility implications
2. **Don't modify `vendor/`** — always use `go mod tidy && go mod vendor`
3. **Don't modify `pkg/generated/`** — always use `make generate-clients`
4. **Don't modify `zz_generated.deepcopy.go`** — always use `make generate`
5. **Don't skip `make verify`** before considering work complete
6. **Don't log secrets** — scheduler config, TLS certificates, or auth tokens must never appear in logs
7. **Don't modify OWNERS files** without explicit direction from maintainers
8. **Don't modify embedded templates** in `bindata/assets/` without updating the reconciler logic that processes them

### DO
1. **Run `make verify`** before submitting any changes
2. **Run `make test-unit`** to ensure tests pass
3. **Use structured logging** via klog with appropriate verbosity levels
4. **Follow Kubernetes API conventions** for CRD status conditions
5. **Handle errors gracefully** and return meaningful error messages
6. **Use the library-go controller factory pattern** — do not introduce controller-runtime
7. **Keep `deploy/`, `manifests/`, and `test/e2e/bindata/` in sync** — CRD and RBAC are copied across these locations (use `make regen-crd` and `make sync-rbac`)
8. **Document architectural decisions** in ARCHITECTURE.md

## Non-Obvious Internals

- **`controllercmd` framework:** The entry point chain (`cmd/` → `pkg/cmd/operator/` → `pkg/operator/starter.go`) passes through library-go's `controllercmd.ControllerCommandConfig`, which handles leader election, signal handling, health checks, and serving info.
- **Template substitution in Deployment:** The reconciler reads the embedded `deployment.yaml` template, replaces `${IMAGE}` and `${CONFIGMAP}` placeholders, injects log verbosity and TLS args, and applies topology settings — all in Go code, not via any templating engine.
- **HA mode node counting:** In `HighlyAvailable` mode, the reconciler counts nodes matching the `nodeSelector` and sets replicas to `min(matchingNodes, maxReplicas)`. If `maxReplicas` is 0, it returns an error.
- **Resource version tracking:** The reconciler stores resource versions of the ConfigMap and other dependencies as annotations on the Deployment's pod template. This forces a rolling update when those resources change, even if the Deployment spec itself is unchanged.
- **OTE test binary:** The e2e tests are compiled into a separate binary (`cmd/secondary-scheduler-operator-tests-ext/`) using the OpenShift Tests Extension framework and shipped gzipped inside the operator image. CI extracts and runs it.
- **CRD lives in three places:** `manifests/secondary-scheduler-operator.crd.yaml` is the source of truth. `make regen-crd` copies it to `deploy/00_secondary-scheduler-operator.crd.yaml` and `test/e2e/bindata/assets/00_secondary-scheduler-operator.crd.yaml`.

### Updating Dependencies

1. Update `go.mod`: `go get <module>@<version> && go mod tidy`
2. Vendor: `go mod vendor`
3. Verify: `make verify && make test-unit`

## Testing

- **Unit tests:** Co-located `*_test.go` files in `pkg/operator/`, table-driven, run with `make test-unit` or `go test ./pkg/... ./cmd/...`.
- **E2E tests:** `test/e2e/` — deploys the operator to a real cluster, creates a `SecondaryScheduler` CR, verifies pod scheduling, metrics, and HA mode. Run with `make test-e2e` (requires cluster).

## Additional Resources

- [ARCHITECTURE.md](ARCHITECTURE.md) — Complete system design, components, and technical details
- [README.md](README.md) — User-facing documentation and getting started guide
- [OpenShift library-go](https://github.com/openshift/library-go) — Controller factory patterns and operator helpers
- [Kubernetes Scheduling Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/) — Plugin framework for custom schedulers
- [Scheduler Plugins](https://github.com/kubernetes-sigs/scheduler-plugins) — Community scheduler plugins (e.g., Trimaran)
