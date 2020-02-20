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

run: build
	./gnmi-gateway -EnableServer -EnablePromethetus -OpenConfigDirectory=./oc-models/

sync:
	go get ./...

test:
	go test -mod=vendor ./...

update:
	go mod tidy
	go get -u ./...
