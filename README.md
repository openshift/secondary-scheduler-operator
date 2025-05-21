# README

## FBC catalog rendering

$ cd v4.19
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic catalog-template.json --migrate-level bundle-object-to-csv-metadata > catalog/openshift-secondary-scheduler-operator/catalog.json
