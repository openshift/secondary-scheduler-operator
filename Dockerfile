FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.25 as builder
WORKDIR /go/src/github.com/openshift/secondary-scheduler-operator
COPY . .
RUN make build --warn-undefined-variables \
    && gzip secondary-scheduler-operator-tests-ext

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:d91be7cea9f03a757d69ad7fcdfcd7849dba820110e7980d5e2a1f46ed06ea3b
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/secondary-scheduler-operator /usr/bin/
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/secondary-scheduler-operator-tests-ext.gz /usr/bin/
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/LICENSE /licenses/.

LABEL io.k8s.display-name="OpenShift Secondary-scheduler Operator based on RHEL 9" \
      io.k8s.description="This is a component of OpenShift and manages the secondary scheduler based on RHEL 9" \
      com.redhat.component="secondary-scheduler-operator-container" \
      name="openshift-secondary-scheduler-operator/secondary-scheduler-rhel9-operator" \
      cpe="cpe:/a:redhat:openshift_secondary_scheduler:1.5::el9" \
      release="1.6.0" \
      version="1.6.0" \
      url="https://github.com/openshift/secondary-scheduler-operator" \
      vendor="Red Hat, Inc." \
      summary="secondary-scheduler-operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="openshift,secondary-scheduler-operator" \
      description="secondary-scheduler-operator-container" \
      maintainer="AOS workloads team, <aos-workloads-staff@redhat.com>"

USER nobody
