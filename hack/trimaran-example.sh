#!/bin/bash

# This shell script substitues the operator's default image urls to a user's image url
# And configure the necessary Prometheus URL and token to demo installation of a Trimaran
# scheduler by the secondary-scheduler-operator

echo "dumping ENV vars"
echo "QUAY_REGISTRY=${QUAY_REGISTRY}"

OLD_OPERATOR_IMAGE_URL="quay.io/openshift/secondary-scheduler-operator:4.9"
NEW_OPERATOR_IMAGE_URL="quay.io/${QUAY_REGISTRY}/secondary-scheduler-operator:4.9"

echo "OLD_OPERATOR_IMAGE_URL=${OLD_OPERATOR_IMAGE_URL}"
echo "NEW_OPERATOR_IMAGE_URL=${NEW_OPERATOR_IMAGE_URL}"
OPERATOR_DEPLOYMENT_FILE="./_tmp/05_deployment.yaml"
echo "OPERATOR_DEPLOYMENT_FILE=${OPERATOR_DEPLOYMENT_FILE}"

echo "Replacing ${OLD_OPERATOR_IMAGE_URL} with ${NEW_OPERATOR_IMAGE_URL} in ${OPERATOR_DEPLOYMENT_FILE}!"
sed "s,${OLD_OPERATOR_IMAGE_URL},${NEW_OPERATOR_IMAGE_URL},g" -i "${OPERATOR_DEPLOYMENT_FILE}"

# Obtain PROM_HOST, PROM_TOKEN and export env vars.
PROM_HOST=`oc get routes prometheus-k8s -n openshift-monitoring -ojson |jq ".status.ingress"|jq ".[0].host"|sed 's/"//g'`
PROM_URL="https://${PROM_HOST}"
TOKEN_NAME=`oc get secret -n openshift-monitoring|awk '{print $1}'|grep prometheus-k8s-token -m 1`
PROM_TOKEN=`oc describe secret $TOKEN_NAME -n openshift-monitoring|grep "token:"|cut -d: -f2|sed 's/^ *//g'`

# Prometheus placeholders
PROM_URL_PLACEHOLDER="\${PROM_URL}"
PROM_TOKEN_PLACEHOLDER="\${PROM_TOKEN}"
echo "PROM_URL_PLACEHOLDER=${PROM_URL_PLACEHOLDER}"
echo "PROM_URL=${PROM_URL}"
echo "PROM_TOKEN_PLACEHOLDER=${PROM_TOKEN_PLACEHOLDER}"
echo "PROM_TOKEN=${PROM_TOKEN}"

echo "Updating the Prometheus placeholders in configmap!"
CONFIGMAP_FILE="./_tmp/06_configmap.yaml"
sed "s,${PROM_URL_PLACEHOLDER},${PROM_URL},g" -i "${CONFIGMAP_FILE}"
sed "s,${PROM_TOKEN_PLACEHOLDER},${PROM_TOKEN},g" -i "${CONFIGMAP_FILE}"

