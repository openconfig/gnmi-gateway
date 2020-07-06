VERSION := "$(shell git describe --tags)-$(shell git rev-parse --short HEAD)"
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X github.com/openconfig/gnmi-gateway/gateway.Version=$(VERSION)
GOLDFLAGS += -X github.com/openconfig/gnmi-gateway/gateway.Buildtime=$(BUILDTIME)
GOFLAGS = -ldflags "$(GOLDFLAGS)"

build: clean test
	go build -o gnmi-gateway $(GOFLAGS) .
	./gnmi-gateway -version

clean:
	rm -f gnmi-gateway

debug: build
	./gnmi-gateway -EnableGNMIServer -ServerTLSCert=server.crt -ServerTLSKey=server.key -PProf -CPUProfile=cpu.pprof

download:
	if [ -d ./oc-models ]; then git --git-dir=./oc-models/.git pull; else git clone https://github.com/openconfig/public.git oc-models; fi

run: build
	./gnmi-gateway -EnableGNMIServer -ServerTLSCert=server.crt -ServerTLSKey=server.key

sync:
	go get ./...

test:
	go test ./...

tls:
	openssl ecparam -genkey -name secp384r1 -out server.key
	openssl req -new -x509 -sha256 -key server.key -out server.crt -days 3650 -subj "/CN=selfsigned.gnmi-gateway.local"

update:
	go mod tidy
	go get -u ./...
