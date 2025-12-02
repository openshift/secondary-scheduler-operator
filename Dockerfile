FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.24 as builder
WORKDIR /go/src/github.com/openshift/secondary-scheduler-operator
COPY . .
RUN make build --warn-undefined-variables \
    && gzip secondary-scheduler-operator-tests-ext

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:161a4e29ea482bab6048c2b36031b4f302ae81e4ff18b83e61785f40dc576f5d
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/secondary-scheduler-operator /usr/bin/
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/secondary-scheduler-operator-tests-ext.gz /usr/bin/
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/LICENSE /licenses/.

LABEL io.k8s.display-name="OpenShift Secondary-scheduler Operator based on RHEL 9" \
      io.k8s.description="This is a component of OpenShift and manages the secondary scheduler based on RHEL 9" \
      com.redhat.component="secondary-scheduler-operator-container" \
      name="openshift-secondary-scheduler-operator/secondary-scheduler-rhel9-operator" \
      cpe="cpe:/a:redhat:openshift_secondary_scheduler:1.5::el9" \
      release="1.5.1" \
      version="1.5.1" \
      url="https://github.com/openshift/secondary-scheduler-operator" \
      vendor="Red Hat, Inc." \
      summary="secondary-scheduler-operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="openshift,secondary-scheduler-operator" \
      description="secondary-scheduler-operator-container" \
      maintainer="AOS workloads team, <aos-workloads-staff@redhat.com>"

USER nobody
