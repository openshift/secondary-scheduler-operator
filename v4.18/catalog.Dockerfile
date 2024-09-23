# The base image is expected to contain
# /bin/opm (with a serve subcommand) and /bin/grpc_health_probe
# FIXME when 4.17 is released
# FIXME generate olm.csv.metadata for 4.17
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.16

# Configure the entrypoint and command
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]

ADD licenses/ /licenses/
# Copy declarative config root into image at /configs and pre-populate serve cache
ADD catalog/ /configs
RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]

# Set DC-specific label for the location of the DC root directory
# in the image
LABEL operators.operatorframework.io.index.configs.v1=/configs