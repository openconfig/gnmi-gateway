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
	rm -f cover.out

cover:
	go test -count=1 -cover -coverprofile=cover.out ./...
	go tool cover -func=cover.out

debug: build
	./gnmi-gateway -PProf -CPUProfile=cpu.pprof -EnableGNMIServer -ServerTLSCert=server.crt -ServerTLSKey=server.key -TargetLoaders=json -TargetJSONFile=targets.json

download:
	if [ -d ./oc-models ]; then git --git-dir=./oc-models/.git pull; else git clone https://github.com/openconfig/public.git oc-models; fi

integration:
	echo "Integration Test Note: Make sure you have Zookeeper running on 127.0.0.1:2181 (see README)."
	go test -tags=integration -count=1 -cover ./...

run: build
	./gnmi-gateway -EnableGNMIServer -ServerTLSCert=server.crt -ServerTLSKey=server.key -TargetLoaders=json -TargetJSONFile=targets.json

sync:
	go get ./...

targets:
	cp targets-example.json targets.json

test:
	go test -count=1 -cover ./...

tls:
	openssl ecparam -genkey -name secp384r1 -out server.key
	openssl req -new -x509 -sha256 -key server.key -out server.crt -days 3650 -subj "/CN=selfsigned.gnmi-gateway.local"

update:
	go mod tidy
	go get -u ./...
