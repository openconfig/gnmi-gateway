[![Release](https://img.shields.io/github/release/openconfig/gnmi-gateway.svg)](https://github.com/openconfig/gnmi-gateway/releases/latest)
[![Testing](https://github.com/openconfig/gnmi-gateway/workflows/Testing/badge.svg?branch=release)](https://github.com/openconfig/gnmi-gateway/actions?query=workflow%3ATesting+branch%3Arelease)
[![Go Report Card](https://goreportcard.com/badge/github.com/openconfig/gnmi-gateway)](https://goreportcard.com/report/github.com/openconfig/gnmi-gateway)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/openconfig/gnmi-gateway)](https://pkg.go.dev/github.com/openconfig/gnmi-gateway/gateway)

⚠ Experimental. Please take note that this is a pre-release version.

# gNMI Gateway

**gnmi-gateway** is a distributed and highly available service for connecting
to multiple [gNMI][1] targets. Currently only the [gNMI Subscribe][2] RPC
is supported.

Common use-cases are:
- Provide multiple streams to gNMI clients while maintaining a single
  connection to gNMI targets.
- Provide highly available streams to gNMI clients.
- Distribute gNMI target connections among multiple servers.
- Export gNMI streams to other data formats and protocols.
- Dynamically form connections to gNMI targets based on data in other systems
  (e.g., your NMS, or network inventory, etc).
  
  
## Design

### Overview

gnmi-gateway is written in Golang and is designed to be easily extendable
for users and organizations interested in utilizing gNMI data (modeled with
[OpenConfig][5]). However, if you aren't interested in writing your own code
there are a few built-in components to make it easy to use from the
command-line.

gnmi-gateway connects to gNMI targets based on data received from
**Target Loaders**. gNMI Notification messages are then forwarded to the
gnmi-gateway cache, gNMI clients with relevant subscriptions, and
**Exporters** which may forward data to other systems or protocols.

### Target Loaders

Target Loaders are components that are used to generate target connection
configurations that are sent to the connection manager. Target Loaders
and the connection manager communicate using the [target.proto][6] model
found in the github.com/openconfig/gnmi repository. See
[loaders/loader.go](./gateway/loaders/loader.go) for details on how to
implement the TargetLoader interface.

### Exporters

Exporters are components of gnmi-gateway that are used to convert gNMI data
into other formats and protocols for use by other systems. Some simple
examples would be sending gNMI notifications to a Kafka stream or
storing gNMI messages in a data store. Exporters will receive each gNMI message
in the stream as it is received but also have access to [query][7] the local
gNMI cache.

Exporters may be run on the same servers as your gnmi-gateway target
connections or you can run exporters on a server acting as clients to another
gnmi-gateway cluster. This allows for some flexibility in your deployment
design.

See [exporters/exporter.go](./gateway/exporters/exporter.go) for details on
how to implement the Exporter interface.


## Documentation

Most of the documentation resides in this repo. Please feel welcome to file
a Github issue if you have question.

See the [godoc pages][8] for documentation and usage examples.


## Pre-requisites
- Golang 1.13 or newer
- A target that supports gNMI Subscribe. This is usually a network router or
  switch.
- A running instance of [Apache Zookeeper][3]. If you only want to run
  a single instance of gnmi-gateway (i.e. without failover)
  you don't need Zookeeper. See the development instructions below for how
  to set up a Zookeeper Docker container.
  
  
## Source Install / Run Instructions

These are the commands that would be used to start gnmi-gateway on a Linux
install that has `make` installed. If you are not on a platform that is
compatible with the Makefile the commands inside the Makefile should translate
to other platforms that support Golang.

1.  `git clone github.com/openconfig/gnmi-gateway`
2.  `cd gnmi-gateway`
3.  `make tls` (If you have your own TLS server certificates
    you may use them instead. It is recommended that you do not use these
    generated self-signed certificates in production.)
4.  Copy `targets-example.json` to `targets.json` and modify it to match your
    gNMI target. You need to modify the target name, target address, and
    credentials.
5.  `make run`
6.  gnmi-gateway should now be running. If you are unable to get gnmi-gateway
    running at this point please check the `./gnmi-gateway -help` dialog
    for tips (assuming the binary built) and then [file an issue on Github][4]
    if you are still unsuccessful.

  
## Examples

#### gNMI to Prometheus Exporter

gnmi-gateway ships with an Exporter that allows you to export
OpenConfig-modeled gNMI data to Prometheus.

See the README in [examples/gnmi-prometheus/](./examples/gnmi-prometheus/)
for details on how to start the gnmi-gateway Docker container and connect it
to a Prometheus Docker container.


## Production Deployment

It is recommended that gnmi-gateway be deployed to immutable infrastructure
such as Kubernetes or an AWS EC2 instance (or something else). New version tags
can be retrieved from Github and deployed with your configuration.

Most configuration can be done via command-line flags. If you need more complex
options for configuring gnmi-gateway or want to configure the gateway at
runtime you can create a .go file that imports the gateway package and create a
configuration.GatewayConfig instance, passing that to gateway.NewGateway, and 
then calling StartGateway. For an example of how this is done you can look at
the code in Main() in gateway/main.go.

To enable clustering of gnmi-gateway you will need an instance (or ideally a
cluster) of Apache Zookeeper accessible to all of the gnmi-gateway instances.
Additionally all of the gnmi-gateway instances in the cluster must be able
to reach each other over the network.

It is recommended that you limit the deployment of a cluster to a single
geographic region or a single geographic area with consistent latency for ideal
performance. You may run instances of gnmi-gateway distributed globally but
may encounter performance issues. You'll likely encounter timeout issues
with Zookeeper as your latency begins to approach the Zookeeper `tickTime`.


## Development
Check the [to-do](./docs/TODO.md) list for any open known issues or
new features.

#### Start Zookeeper for development

This should ony be used for development and not for production. The
container will maintain no state; you will have a completely empty
Zookeeper tree when this starts/restarts. To start zookeeper and expose the
server on `127.0.0.1:2181` run:

```shell script
docker run -d -p 2181:2181 zookeeper
```

#### Test the code

You can test the code by running `make test`.

You can run integration tests by running `make integration`. (Ensure you have
Zookeeper running on `127.0.0.1:2181`.)

You can run test coverage by running `make cover`.

#### Build the code

You can build the `gnmi-gateway` binary by running `make build`.

#### Contributions

Please make any changes in a separate fork and make a PR to the `release`
branch when your changes are ready. Tags for new release versions will be cut
from the `release` branch.


[1]: https://github.com/openconfig/gnmi
[2]: https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md#35-subscribing-to-telemetry-updates
[3]: https://zookeeper.apache.org/
[4]: https://github.com/openconfig/gnmi-gateway/issues
[5]: https://github.com/openconfig/public/tree/master/release
[6]: https://github.com/openconfig/gnmi/blob/master/proto/target/target.proto
[7]: https://github.com/openconfig/gnmi/blob/master/cache/cache.go#L143
[8]: https://godoc.org/github.com/openconfig/gnmi-gateway
