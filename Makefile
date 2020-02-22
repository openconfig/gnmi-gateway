VERSION := "$(shell git describe --tags)-$(shell git rev-parse --short HEAD)"
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X gateway.Version=$(VERSION)
GOLDFLAGS += -X gateway.Buildtime=$(BUILDTIME)
GOFLAGS = -ldflags "$(GOLDFLAGS)"

build: clean test
	git clone https://github.com/openconfig/public.git oc-models
	go build -o gnmi-gateway $(GOFLAGS) .
	./gnmi-gateway -version

clean:
	rm -rf oc-models
	rm -f gnmi-gateway

debug: build
	./gnmi-gateway -EnableServer -EnablePrometheus -OpenConfigDirectory=./oc-models/ -ServerTLSCert=server.crt -ServerTLSKey=server.key -PProf

run: build
	./gnmi-gateway -EnableServer -EnablePrometheus -OpenConfigDirectory=./oc-models/

sync:
	go get ./...

test:
	go test ./...

tls:
	openssl ecparam -genkey -name secp384r1 -out server.key
	openssl req -new -x509 -sha256 -key server.key -out server.crt -days 3650

update:
	go mod tidy
	go get -u ./...
