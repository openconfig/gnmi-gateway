FROM golang:1.18-alpine

ENV INSTALL_DIR /opt/gnmi-gateway

WORKDIR "${INSTALL_DIR}"
COPY . "${INSTALL_DIR}"

RUN apk add --update make gcc g++ git openssl
RUN make build
RUN make download
RUN make tls
RUN ./gnmi-gateway -version

CMD ["./gnmi-gateway", \
    "-TargetLoaders=json", \
    "-TargetJSONFile=./targets.json", \
    "-TargetLimit=300" \
    "-EnableGNMIServer", \
    "-OpenConfigDirectory=./oc-models/", \ 
    "-ServerTLSCert=server.crt", \
    "-ServerTLSKey=server.key"]
