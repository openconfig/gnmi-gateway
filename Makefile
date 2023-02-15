VERSION := "$(shell git describe --tags)-$(shell git rev-parse --short HEAD)"
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X github.com/mspiez/gnmi-gateway/gateway.Version=$(VERSION)
GOLDFLAGS += -X github.com/mspiez/gnmi-gateway/gateway.Buildtime=$(BUILDTIME)
GOFLAGS = -ldflags "$(GOLDFLAGS)"

.PHONY: build release

build: clean
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

godoc:
	godoc -http=":6060"

imports:
	goimports -local github.com/openconfig/gnmi-gateway -w -l $(shell find . -type f -name '*.go' -not -path './vendor/*' -not -path '*.pb.go')

integration:
	echo "Integration Test Note: Make sure you have Zookeeper running on 127.0.0.1:2181 (see README)."
	go test -tags=integration -count=1 -cover ./...

lint: imports
	go fmt ./ ./gateway/...
	go vet

release:
	mkdir -p release
	rm -f release/gnmi-gateway release/gnmi-gateway.exe
ifeq ($(shell go env GOOS), windows)
	go build -o release/gnmi-gateway.exe $(GOFLAGS) .
	cd release; zip -m "gnmi-gateway-$(shell git describe --tags --abbrev=0)-$(shell go env GOOS)-$(shell go env GOARCH).zip" gnmi-gateway.exe
else
	go build -o release/gnmi-gateway $(GOFLAGS) .
	cd release; zip -m "gnmi-gateway-$(shell git describe --tags --abbrev=0)-$(shell go env GOOS)-$(shell go env GOARCH).zip" gnmi-gateway
endif
	cd release; sha256sum "gnmi-gateway-$(shell git describe --tags --abbrev=0)-$(shell go env GOOS)-$(shell go env GOARCH).zip" > "gnmi-gateway-$(shell git describe --tags --abbrev=0)-$(shell go env GOOS)-$(shell go env GOARCH).zip.sha256"

run: build
	./gnmi-gateway -EnableGNMIServer -ServerTLSCert=server.crt -ServerTLSKey=server.key -TargetLoaders=json -TargetJSONFile=targets.json

sync:
	go get ./...

targets:
	cp targets-example.json targets.json

test: clean
	go test -count=1 -cover ./...

tls:
	openssl ecparam -genkey -name secp384r1 -out server.key
	openssl req -new -x509 -sha256 -key server.key -out server.crt -days 3650 -subj "/CN=selfsigned.gnmi-gateway.local"

update:
	go mod tidy
	go get -u ./...
