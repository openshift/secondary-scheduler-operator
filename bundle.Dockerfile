FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.24 as builder
WORKDIR /go/src/github.com/openshift/secondary-scheduler-operator
COPY . .

RUN mkdir licenses
COPY ./LICENSE licenses/.

ARG OPERATOR_IMAGE=registry.redhat.io/openshift-secondary-scheduler-operator/secondary-scheduler-rhel9-operator@sha256:25f38a4c8cf3932fd6759a0bad889a7253b3d5d5a0a654abe5a348927eaf630f
ARG REPLACED_OPERATOR_IMG=registry-proxy.engineering.redhat.com/rh-osbs/secondary-scheduler-rhel9-operator:latest

RUN hack/replace-image.sh manifests ${REPLACED_OPERATOR_IMG} ${OPERATOR_IMAGE}

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:bb08f2300cb8d12a7eb91dddf28ea63692b3ec99e7f0fa71a1b300f2756ea829

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
LABEL name="openshift-secondary-scheduler-operator/secondary-scheduler-operator-bundle"
LABEL cpe="cpe:/a:redhat:openshift_secondary_scheduler:1.5::el9"
LABEL release="1.5.1"
LABEL version="1.5.1"
LABEL url="https://github.com/openshift/secondary-scheduler-operator"
LABEL vendor="Red Hat, Inc."
LABEL summary="Secondary scheduler support for OpenShift"
LABEL io.openshift.expose-services=""
LABEL io.k8s.display-name="Openshift Secondary Scheduler Operator Bundle"
LABEL io.k8s.description="This is a bundle image for Secondary Scheduler"
LABEL io.openshift.tags="openshift,secondary-scheduler-operator"
LABEL com.redhat.delivery.operator.bundle=true
LABEL com.redhat.openshift.versions="v4.20"
LABEL com.redhat.delivery.appregistry=true
LABEL maintainer="AOS workloads team, <aos-workloads-staff@redhat.com>"

USER 1001
