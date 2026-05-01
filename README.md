# README

## FBC catalog rendering

```
$ export REGISTRY_AUTH_FILE=...
$ export DOCKER_CONFIG=...
$ opm alpha render-template basic v4.21/catalog-template.yaml --migrate-level bundle-object-to-csv-metadata > v4.21/catalog/openshift-secondary-scheduler-operator/catalog.json
$ opm validate v4.21/catalog/openshift-secondary-scheduler-operator
```
