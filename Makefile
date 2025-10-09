all: build
.PHONY: all

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/images.mk \
	targets/openshift/codegen.mk \
	targets/openshift/deps.mk \
	targets/openshift/crd-schema-gen.mk \
)

# Exclude e2e tests from unit testing
GO_TEST_PACKAGES :=./pkg/... ./cmd/...
# Exclude tests-ext from default build (built separately via tests-ext-build target)
GO_BUILD_PACKAGES :=./cmd/secondary-scheduler-operator
GO_BUILD_FLAGS :=-tags strictfipsruntime

IMAGE_REGISTRY :=registry.svc.ci.openshift.org

CODEGEN_OUTPUT_PACKAGE :=github.com/openshift/secondary-scheduler-operator/pkg/generated
CODEGEN_API_PACKAGE :=github.com/openshift/secondary-scheduler-operator/pkg/apis
CODEGEN_GROUPS_VERSION :=secondaryscheduler:v1

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target name
# $2 - image ref
# $3 - Dockerfile path
# $4 - context directory for image build
$(call build-image,ocp-secondary-scheduler-operator,$(IMAGE_REGISTRY)/ocp/4.9:secondary-scheduler-operator, ./Dockerfile.rhel7,.)

$(call verify-golang-versions,Dockerfile.rhel7)

$(call add-crd-gen,secondaryscheduler,./pkg/apis/secondaryscheduler/v1,./manifests/4.9,./manifests/4.9)

test-e2e: GO_TEST_PACKAGES :=./test/e2e
# the e2e imports pkg/cmd which has a data race in the transport library with the library-go init code
test-e2e: GO_TEST_FLAGS :=-v
test-e2e: test-unit
.PHONY: test-e2e

regen-crd:
	go build -o _output/tools/bin/controller-gen ./vendor/sigs.k8s.io/controller-tools/cmd/controller-gen
	cp manifests/secondary-scheduler-operator.crd.yaml manifests/operator.openshift.io_secondaryschedulers.yaml
	./_output/tools/bin/controller-gen crd paths=./pkg/apis/secondaryscheduler/v1/... schemapatch:manifests=./manifests output:crd:dir=./manifests
	mv manifests/operator.openshift.io_secondaryschedulers.yaml manifests/secondary-scheduler-operator.crd.yaml

generate: update-codegen-crds generate-clients
.PHONY: generate

generate-clients:
	GO=GO111MODULE=on GOFLAGS=-mod=readonly hack/update-codegen.sh
.PHONY: generate-clients

clean:
	$(RM) ./secondary-scheduler-operator
	$(RM) -r ./_tmp
.PHONY: clean

# OpenShift Tests Extension variables
TESTS_EXT_BINARY ?= secondary-scheduler-operator-tests-ext
TESTS_EXT_PACKAGE ?= ./cmd/secondary-scheduler-operator-tests-ext
TESTS_EXT_LDFLAGS ?= -X 'main.CommitFromGit=$(shell git rev-parse --short HEAD)' \
                     -X 'main.BuildDate=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)' \
                     -X 'main.GitTreeState=$(shell if git diff-index --quiet HEAD --; then echo clean; else echo dirty; fi)'

# Build the openshift-tests-extension binary
.PHONY: tests-ext-build
tests-ext-build:
	GOOS=$(GOOS) GOARCH=$(GOARCH) GO_COMPLIANCE_POLICY=exempt_all CGO_ENABLED=0 \
        go build -o $(TESTS_EXT_BINARY) -ldflags "$(TESTS_EXT_LDFLAGS)" $(TESTS_EXT_PACKAGE)

# Update test metadata
.PHONY: tests-ext-update
tests-ext-update:
	./$(TESTS_EXT_BINARY) update

# Clean tests extension artifacts
.PHONY: tests-ext-clean
tests-ext-clean:
	rm -f $(TESTS_EXT_BINARY) $(TESTS_EXT_BINARY).gz

# Run tests extension help
.PHONY: tests-ext-help
tests-ext-help:
	./$(TESTS_EXT_BINARY) --help
# Run sanity test
.PHONY: tests-ext-sanity
tests-ext-sanity:
	./$(TESTS_EXT_BINARY) run-suite "openshift/secondary-scheduler-operator/conformance/parallel"

# List available tests
.PHONY: tests-ext-list
tests-ext-list:
	./$(TESTS_EXT_BINARY) list tests

# Show extension info
.PHONY: tests-ext-info
tests-ext-info:
	./$(TESTS_EXT_BINARY) info

