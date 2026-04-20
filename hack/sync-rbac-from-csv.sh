#!/bin/bash

set -e

CSV_FILE="${1:-manifests/cluster-secondary-scheduler-operator.clusterserviceversion.yaml}"
CLUSTERROLE_OUTPUT="${2:-deploy/02_clusterrole.yaml}"
ROLE_OUTPUT="${3:-deploy/03_operatorrole.yaml}"

# Check if yq is available
if ! command -v yq &> /dev/null; then
    echo "Error: yq is required but not installed. Please install yq."
    exit 1
fi

# Extract clusterPermissions rules from CSV and create ClusterRole YAML
yq eval '
  {
    "kind": "ClusterRole",
    "apiVersion": "rbac.authorization.k8s.io/v1",
    "metadata": {
      "name": "secondary-scheduler-operator"
    },
    "rules": .spec.install.spec.clusterPermissions[0].rules
  }
' "${CSV_FILE}" > "${CLUSTERROLE_OUTPUT}"

echo "Generated ${CLUSTERROLE_OUTPUT} from ${CSV_FILE}"

# Extract permissions rules from CSV and create Role YAML
yq eval '
  {
    "apiVersion": "rbac.authorization.k8s.io/v1",
    "kind": "Role",
    "metadata": {
      "name": "secondary-scheduler-operator",
      "namespace": "openshift-secondary-scheduler-operator",
      "annotations": {
        "include.release.openshift.io/self-managed-high-availability": "true",
        "include.release.openshift.io/single-node-developer": "true"
      }
    },
    "rules": .spec.install.spec.permissions[0].rules
  }
' "${CSV_FILE}" > "${ROLE_OUTPUT}"

echo "Generated ${ROLE_OUTPUT} from ${CSV_FILE}"
