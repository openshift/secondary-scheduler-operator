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
