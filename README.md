# README

## FBC catalog rendering

To initiliaze catalog-template.json

```sh
$ opm migrate registry.redhat.io/redhat/redhat-operator-index:v4.13 ./catalog-migrate
$ mkdir -p v4.13/catalog/openshift-secondary-scheduler-operator
$ opm alpha convert-template basic ./catalog-migrate/openshift-secondary-scheduler-operator/catalog.json > v4.13/catalog-template.json
```

To update the catalog

```
$ cd v4.13
$ opm alpha render-template basic catalog-template.json > catalog/openshift-secondary-scheduler-operator/catalog.json
```

## Releases

| osso version | bundle image                                                     |
| ------------ | ---------------------------------------------------------------- |
| 1.1.2        | daea4461ca6a1903f2e2a1470df8fdfe413106e84e0b36789e0fb0e2bbdba333 |
| 1.1.3        | 51458b1eafc32dd920558e757506e9b71856b5b47744284c961c5430766536b2 |
| 1.1.4        | c3180b19acf3b2fefc93a1620917b5f94731ecfe87457c811359e0aa0d25f4ae |
| 1.1.5        | 0bd806d5f8f87b035258540549a5a400cf1b9d20d513ceb8b244b8cb589da852 |
