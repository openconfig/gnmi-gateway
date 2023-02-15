FROM golang:1.14-alpine

ENV INSTALL_DIR /opt/gnmi-gateway

RUN apk add --update make gcc g++ git openssl

WORKDIR "${INSTALL_DIR}"
COPY . "${INSTALL_DIR}"

RUN make build
# RUN make download
RUN make tls
RUN ./gnmi-gateway -version

CMD ["./gnmi-gateway", \
    "-TargetLoaders=json", \
    "-TargetJSONFile=./targets.json", \
    # "-TargetLoaders=simple", \
    # "-SimpleFile=targets.yaml", \
    "-EnableGNMIServer", \
    "-Exporters=prometheus", \
    # "-Exporters=debug", \
    "-Exporters=nautobot", \
    "-OpenConfigDirectory=./oc-models/", \
    "-ServerTLSCert=server.crt", \
    "-ServerTLSKey=server.key"]
