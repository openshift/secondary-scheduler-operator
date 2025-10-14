# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.14 ./catalog-migrate
$ mkdir -p v4.14/catalog/openshift-secondary-scheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/openshift-secondary-scheduler-operator/catalog.json > v4.14/catalog-template.json
```

To update the catalog

```
$ cd v4.14
$ opm alpha render-template basic catalog-template.json > catalog/openshift-secondary-scheduler-operator/catalog.json
```
