FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.25 as builder
WORKDIR /go/src/github.com/openshift/secondary-scheduler-operator
COPY . .

RUN mkdir licenses
COPY ./LICENSE licenses/.

ARG OPERATOR_IMAGE=registry.redhat.io/openshift-secondary-scheduler-operator/secondary-scheduler-rhel9-operator@sha256:b749e285aa74a4482807f85ae826e6a78c259624bee0ea884b7f78e1d1537336
ARG REPLACED_OPERATOR_IMG=registry-proxy.engineering.redhat.com/rh-osbs/secondary-scheduler-rhel9-operator:latest

RUN hack/replace-image.sh manifests ${REPLACED_OPERATOR_IMG} ${OPERATOR_IMAGE}
RUN sed -i "s/createdAt: \".*\"/createdAt: \"$(date -I)\"/" manifests/cluster-secondary-scheduler-operator.clusterserviceversion.yaml

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:1d7c5517a4a1a8e2688620b39ee980e82505ca1ab7ae5541b5463120ae9b3897

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
LABEL cpe="cpe:/a:redhat:openshift_secondary_scheduler:1.6::el9"
LABEL release="1.6.0"
LABEL version="1.6.0"
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
