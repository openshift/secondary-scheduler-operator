FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_8_1.18 as builder
WORKDIR /go/src/github.com/openshift/secondary-scheduler-operator
COPY . .
RUN make build --warn-undefined-variables

FROM registry.redhat.io/rhel9-2-els/rhel-minimal:9.2
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/secondary-scheduler-operator /usr/bin/
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/secondary-scheduler-operator/LICENSE /licenses/.

LABEL io.k8s.display-name="OpenShift Secondary-scheduler Operator based on RHEL 9" \
      io.k8s.description="This is a component of OpenShift and manages the secondary scheduler based on RHEL 9" \
      com.redhat.component="secondary-scheduler-operator-container" \
      name="secondary-scheduler-rhel9-operator" \
      summary="secondary-scheduler-operator" \
      io.openshift.expose-services="" \
      io.openshift.tags="openshift,secondary-scheduler-operator" \
      description="secondary-scheduler-operator-container" \
      maintainer="AOS workloads team, <aos-workloads-staff@redhat.com>"

USER nobody
