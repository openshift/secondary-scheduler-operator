# README

## FBC catalog rendering

```
$ export REGISTRY_AUTH_FILE=...
$ export DOCKER_CONFIG=...
$ opm alpha render-template basic v4.20/catalog-template.json --migrate-level bundle-object-to-csv-metadata > v4.20/catalog/openshift-secondary-scheduler-operator/catalog.json
$ opm validate v4.20/catalog/openshift-secondary-scheduler-operator
```

## Releases

| osso version | bundle image                                                     |
| ------------ | ---------------------------------------------------------------- |
| 1.4.0        | c1dca4cb4d901a4ae7798e592fad162ada2c7ab99e8062d712e5415fc2fd5d00 |
| 1.4.1        | 3804fcb77893d9198527b4801e2a778bf612596c9a0b125a0f0f872b23138434 |
| 1.5.0        | a3667f085cb4f043f342d2470471e016fd50ddd4aed83ec88365a01709bc5732 |
