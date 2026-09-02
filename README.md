# README

## FBC catalog rendering

```
$ export REGISTRY_AUTH_FILE=...
$ export DOCKER_CONFIG=...
$ opm alpha render-template basic v4.21/catalog-template.yaml --migrate-level bundle-object-to-csv-metadata > v4.21/catalog/openshift-secondary-scheduler-operator/catalog.json
$ opm validate v4.21/catalog/openshift-secondary-scheduler-operator
```

## Configure mirroring for opm

Under `/etc/containers/registries.conf` (or drop a file inside /etc/containers/registries.conf.d/):
```
[[registry]]
prefix = "registry.redhat.io/openshift-secondary-scheduler-operator"
location = "registry.redhat.io/openshift-secondary-scheduler-operator"
insecure = true

[[registry.mirror]]
prefix = "registry.redhat.io/openshift-secondary-scheduler-operator"
location = "registry.stage.redhat.io/openshift-secondary-scheduler-operator"
insecure = true
```
