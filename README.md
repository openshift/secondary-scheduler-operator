# Secondary Scheduler Operator

The Secondary Scheduler Operator provides the ability to deploy a customized scheduler image developed using the [scheduler plugin framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/) with customized  configuration as a secondary scheduler in OpenShift.

## Deploy the Operator

### Quick Development

1. Build and push the operator image to a registry:
   ```sh
   export QUAY_USER=${your_quay_user_id}
   export IMAGE_TAG=${your_image_tag}
   podman build -t quay.io/${QUAY_USER}/secondary-scheduler-operator:${IMAGE_TAG} .
   podman login quay.io -u ${QUAY_USER}
   podman push quay.io/${QUAY_USER}/secondary-scheduler-operator:${IMAGE_TAG}
   ```

1. Update the image spec under `.spec.template.spec.containers[0].image` field in the `deploy/05_deployment.yaml` Deployment to point to the newly built image

1. Update the `.spec.schedulerImage` field under `deploy/07_secondary-scheduler-operator.cr.yaml` CR to point to a secondary scheduler image

1. Update the `KubeSchedulerConfiguration` under `deploy/06_configmap.yaml` to configure available plugins

1. Apply the manifests from `deploy` directory:
   ```sh
   oc apply -f deploy/
   ```

### Building index image from a bundle image (built in Brew)

This process requires access to the Brew building system.

1. List available bundle images (as IMAGE):
   ```
   $ brew list-builds --package=secondary-scheduler-operator-bundle-container
   ```

1. Get pull secret for selected bundle image (as IMAGE_PULL):
   ```
   $ brew --noauth call --json getBuild IMAGE |jq -r '.extra.image.index.pull[0]'
   ```

1. Build the index image (with IMAGE_TAG):
   ```
   $ opm index add --bundles IMAGE_PULL --tag quay.io/${QUAY_USER}/secondary-scheduler-operator-index:IMAGE_TAG
   ```

### OperatorHub install with custom index image

This process refers to building the operator in a way that it can be installed locally via the OperatorHub with a custom index image

1. Build and push the operator image to a registry:
   ```sh
   export QUAY_USER=${your_quay_user_id}
   export IMAGE_TAG=${your_image_tag}
   podman build -t quay.io/${QUAY_USER}/secondary-scheduler-operator:${IMAGE_TAG} .
   podman login quay.io -u ${QUAY_USER}
   podman push quay.io/${QUAY_USER}/secondary-scheduler-operator:${IMAGE_TAG}
   ```

1. Update the `.spec.install.spec.deployments[0].spec.template.spec.containers[0].image` field in the SSO CSV under `manifests/cluster-secondary-scheduler-operator.clusterserviceversion.yaml` to point to the newly built image.

1. build and push the metadata image to a registry (e.g. https://quay.io):
   ```sh
   podman build -t quay.io/${QUAY_USER}/secondary-scheduler-operator-metadata:${IMAGE_TAG} -f Dockerfile.metadata .
   podman push quay.io/${QUAY_USER}/secondary-scheduler-operator-metadata:${IMAGE_TAG}
   ```

1. build and push image index for operator-registry (pull and build https://github.com/operator-framework/operator-registry/ to get the `opm` binary)
   ```sh
   opm index add --bundles quay.io/${QUAY_USER}/secondary-scheduler-operator-metadata:${IMAGE_TAG} --tag quay.io/${QUAY_USER}/secondary-scheduler-operator-index:${IMAGE_TAG}
   podman push quay.io/${QUAY_USER}/secondary-scheduler-operator-index:${IMAGE_TAG}
   ```

   Don't forget to increase the number of open files, .e.g. `ulimit -n 100000` in case the current limit is insufficient.

1. create and apply catalogsource manifest (notice to change <<QUAY_USER>> and <<IMAGE_TAG>> to your own values)::
   ```yaml
   apiVersion: operators.coreos.com/v1alpha1
   kind: CatalogSource
   metadata:
     name: secondary-scheduler-operator
     namespace: openshift-marketplace
   spec:
     sourceType: grpc
     image: quay.io/<<QUAY_USER>>/secondary-scheduler-operator-index:<<IMAGE_TAG>>
   ```

1. create `openshift-secondary-scheduler-operator` namespace:
   ```
   $ oc create ns openshift-secondary-scheduler-operator
   ```

1. open the console Operators -> OperatorHub, search for  `secondary scheduler operator` and install the operator

1. create CM for the KubeSchedulerConfiguration (the config file has to stored under `config.yaml`). E.g.:
   ```
   cat config.yaml
   apiVersion: kubescheduler.config.k8s.io/v1beta1
   kind: KubeSchedulerConfiguration
   leaderElection:
     leaderElect: false
   profiles:
     - schedulerName: secondary-scheduler
       plugins:
         score:
           disabled:
             - name: NodeResourcesBalancedAllocation
             - name: NodeResourcesLeastAllocated
           enabled:
             - name: TargetLoadPacking
       pluginConfig:
         - name: TargetLoadPacking
           args:
             defaultRequests:
               cpu: "2000m"
             defaultRequestsMultiplier: "1"
             targetUtilization: 70
             metricProvider:
               type: Prometheus
               address: ${PROM_URL}
               token: ${PROM_TOKEN}
   oc create -n openshift-secondary-scheduler-operator configmap secondary-scheduler-config --from-file=config.yaml
   ```
   You can run the following commands to get `PROM_URL` and `PROM_TOKEN` envs from your OpenShift cluster:
   ```sh
   PROM_HOST=`oc get routes prometheus-k8s -n openshift-monitoring -ojson |jq ".status.ingress"|jq ".[0].host"|sed 's/"//g'`
   PROM_URL="https://${PROM_HOST}"
   TOKEN_NAME=`oc get secret -n openshift-monitoring|awk '{print $1}'|grep prometheus-k8s-token -m 1`
   PROM_TOKEN=`oc describe secret $TOKEN_NAME -n openshift-monitoring|grep "token:"|cut -d: -f2|sed 's/^ *//g'`
   ```

1. Create CR for the secondary scheduler operator in the console (`schedulerImage` is set to a scheduler built from upstream https://github.com/kubernetes-sigs/scheduler-plugins repository):
   ```
   apiVersion: operator.openshift.io/v1
   kind: SecondaryScheduler
   metadata:
     name: cluster
     namespace: openshift-secondary-scheduler-operator
   spec:
     managementState: Managed
     schedulerConfig: secondary-scheduler-config
     schedulerImage: k8s.gcr.io/scheduler-plugins/kube-scheduler:v0.22.6
   ```

## Deploying a custom scheduler
To deploy a custom scheduler, you must build and host a container image for
your scheduler using the Kubernetes Scheduler Framework. You can then set the
image with the operator's `spec.schedulerImage` field, like so:
```
$ oc edit secondaryschedulers/secondary scheduler
...
spec:
  schedulerImage: quay.io/myuser/myscheduler:latest
...
```

## Sample CR

A sample CR definition looks like below (the operator expects `cluster` CR under `openshift-secondary-scheduler-operator` namespace):

```yaml
apiVersion: operator.openshift.io/v1
kind: SecondaryScheduler
metadata:
  name: cluster
  namespace: openshift-secondary-scheduler-operator
spec:
  schedulerConfig: secondary-scheduler-config
  schedulerImage: k8s.gcr.io/scheduler-plugins/kube-scheduler:v0.22.6
```

The operator spec provides a `schedulerConfig` and a `schedulerImage` field, which allows users to specify a custom KubeSchedulerConfiguration and a custom scheduler image.
