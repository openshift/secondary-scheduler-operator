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

$(call add-crd-gen,secondaryscheduler,./pkg/apis/secondaryscheduler/v1,./manifests/4.8,./manifests/4.8)

test-e2e: GO_TEST_PACKAGES :=./test/e2e
test-e2e: test-unit
.PHONY: test-e2e

generate: update-codegen-crds generate-clients
.PHONY: generate

generate-clients:
	bash ./vendor/k8s.io/code-generator/generate-groups.sh all github.com/openshift/secondary-scheduler-operator/pkg/generated github.com/openshift/secondary-scheduler-operator/pkg/apis secondaryscheduler:v1
.PHONY: generate-clients

clean:
	$(RM) ./secondary-scheduler-operator
.PHONY: clean
