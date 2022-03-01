FROM golang:1.14-alpine

ENV INSTALL_DIR /opt/gnmi-gateway
ENV ZOOKEEPER_HOSTS ""

WORKDIR "${INSTALL_DIR}"
COPY . "${INSTALL_DIR}"

RUN apk add --update make gcc g++ git openssl
RUN make build
RUN make download
RUN make tls
RUN ./gnmi-gateway -version
