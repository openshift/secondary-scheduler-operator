---
description: Validate a CSV file is consistent
---

## Name

validate-csv

## Synopsis

```
/validate-csv
```

## Description

Make sure a ClusterServiceVersion manifest under manifests/cluster-secondary-scheduler-operator.clusterserviceversion.yaml is consistent.

The validation checks includes:
- .metadata.name suffix version is the same as .spec.version
- .metadata.name suffix version is the same as .labels.olm-status-descriptors suffix version
- .metadata.name suffix version is the same as .metadata.annotations["olm.skipRange"] upper bound version
