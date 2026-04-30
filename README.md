# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.18 ./catalog-migrate
$ mkdir -p v4.18/catalog/openshift-secondary-scheduler-operator
$ opm alpha convert-template basic -o yaml ./catalog-migrate/openshift-secondary-scheduler-operator/catalog.json > v4.18/catalog-template.yaml
```

To update the catalog

```
$ export REGISTRY_AUTH_FILE=...
$ opm alpha render-template basic v4.18/catalog-template.yaml --migrate-level bundle-object-to-csv-metadata > v4.18/catalog/openshift-secondary-scheduler-operator/catalog.json
```

## Releases

| osso version | bundle image                                                     |
| ------------ | ---------------------------------------------------------------- |
| 1.3.0        | 4cea92798fa738944ec3487604e0974e8403e9b31b6330981cb28f12acafed05 |
| 1.3.1        | 082fec20563feb75173232be85f9cca2d564f7bf141a99973e3bd993ec6f6f33 |
| 1.3.2        | f74547b7a26d7da6c5a81dbf9955b8bc934e46626c3c80cd479d63b4853abc43 |
| 1.4.0        | c1dca4cb4d901a4ae7798e592fad162ada2c7ab99e8062d712e5415fc2fd5d00 |
| 1.4.1        | 3804fcb77893d9198527b4801e2a778bf612596c9a0b125a0f0f872b23138434 |
| 1.4.2        | f72d7562cdaddec5d52a83c0eafd24a7e13f10edc8c3cd91c9f63ea5e53627c7 |
