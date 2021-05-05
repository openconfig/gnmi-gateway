FROM golang:1.14-alpine

ENV INSTALL_DIR /opt/gnmi-gateway
ENV INFLUXDB_TARGET http://influxdb:8086
ENV INFLUXDB_TOKEN ""
ENV INFLUXDB_ORG myorg
ENV INFLUXDB_BUCKET telemetry

WORKDIR "${INSTALL_DIR}"
COPY ./ "${INSTALL_DIR}"

RUN apk add --update make gcc g++ git openssl
RUN make build
RUN make download
RUN make tls
RUN ./gnmi-gateway -version

CMD ./gnmi-gateway \
    -TargetLoaders=json \
    -TargetJSONFile=./targets.json \
    -EnableGNMIServer \
    -Exporters=influxdb \
    -ExportersInfluxDBTarget=${INFLUXDB_TARGET} \
    -ExportersInfluxDBToken=${INFLUXDB_TOKEN} \
    -ExportersInfluxDBOrg=${INFLUXDB_ORG} \
    -ExportersInfluxDBBucket=${INFLUXDB_BUCKET} \
    -ServerTLSCert=server.crt \
    -ServerTLSKey=server.key