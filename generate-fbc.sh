for OCP_VERSION in v4.20; do
    echo "OCP_VERSION: ${OCP_VERSION}"
    opm alpha render-template basic $OCP_VERSION/catalog-template.json --migrate-level bundle-object-to-csv-metadata > $OCP_VERSION/catalog/openshift-secondary-scheduler-operator/catalog.json;
done
