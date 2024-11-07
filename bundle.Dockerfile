FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.22 as builder
WORKDIR /go/src/github.com/openshift/secondary-scheduler-operator
COPY . .

RUN mkdir licenses
COPY ./LICENSE licenses/.

ARG OPERATOR_IMAGE=registry.redhat.io/openshift-secondary-scheduler-operator/secondary-scheduler-rhel9-operator@sha256:d91bf78f6015f85e17db3cbc564a6f84d880b808a7084352599d782fb51fd3b6  
ARG REPLACED_OPERATOR_IMG=registry-proxy.engineering.redhat.com/rh-osbs/secondary-scheduler-rhel9-operator:latest

RUN hack/replace-image.sh manifests ${REPLACED_OPERATOR_IMG} ${OPERATOR_IMAGE}

FROM registry.redhat.io/rhel9-4-els/rhel-minimal:9.4

COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/manifests /manifests
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/metadata /metadata
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/licenses /licenses

LABEL operators.operatorframework.io.bundle.mediatype.v1="registry+v1"
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1="openshift-secondary-scheduler-operator"
LABEL operators.operatorframework.io.bundle.channels.v1=stable
LABEL operators.operatorframework.io.bundle.channel.default.v1=stable
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.34.2
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v4

LABEL com.redhat.component="secondary-scheduler-operator-bundle-container"
LABEL description="Secondary scheduler support for OpenShift"
LABEL distribution-scope="public"
LABEL name="secondary-scheduler-operator-metadata-rhel-9"
LABEL release="1.3.2"
LABEL version="1.3.2"
LABEL url="https://github.com/openshift/secondary-scheduler-operator"
LABEL vendor="Red Hat, Inc."
LABEL summary="Secondary scheduler support for OpenShift"
LABEL io.openshift.expose-services=""
LABEL io.k8s.display-name="Openshift Secondary Scheduler Operator Bundle"
LABEL io.k8s.description="This is a bundle image for Secondary Scheduler"
LABEL io.openshift.tags="openshift,secondary-scheduler-operator"
LABEL com.redhat.delivery.operator.bundle=true
LABEL com.redhat.openshift.versions="v4.16"
LABEL com.redhat.delivery.appregistry=true
LABEL maintainer="AOS workloads team, <aos-workloads-staff@redhat.com>"

USER 1001
