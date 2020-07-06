# gNMI Gateway

**gnmi-gateway** is a distributed and highly available service for connecting to multiple gNMI
targets. Currently only the gNMI Subscribe RPC is supported.

Common use-cases are:
- Provide multiple streams to gNMI clients while maintaining a single
  connection to gNMI target.
- Provide highly available streams to gNMI clients.
- Distribute gNMI target connections among multiple servers.


## Pre-requisites
- Golang 1.13 or newer
- A target that supports gNMI Subscribe. This is usually a network router or
  switch.
- A running instance of Apache Zookeeper. If you only want to run
  a single instance of gnmi-gateway (i.e. without failover)
  you don't need Zookeeper. See the development instructions below for how
  to set up a Zookeeper Docker container.
  
  
## Install / Run Instructions

These are the commands that would be used to start gnmi-gateway on a Linux
install that has `make` installed. If you are not on a platform that is
compatible with the Makefile the commands inside the Makefile should translate
to other platforms that support Golang.

1.  `git clone github.com/openconfig/gnmi-gateway`
2.  `cd gnmi-gateway`
3.  `make tls` (Optional. If you have your own TLS server certificates
    you may use them instead. It is recommended that you do not use these
    generated self-signed certificates in production.)
4.  `make run`
5.  gnmi-gateway should now be running, albeit with no targets or
    subscribers. If you are unable to get gnmi-gateway running at this point
    please check the `./gnmi-gateway -help` dialog for tips (assuming the
    binary built) and then file an issue on Gitub if you are still unsuccessful.

  
## Examples

#### gNMI to Prometheus Exporter

gnmi-gateway ships with an Exporter that allows you to export
OpenConfig-modeled gNMI data to Prometheus.

See the README in `examples/gnmi-prometheus/` for details on how to start
the gnmi-gateway Docker container and connect it to a Prometheus Docker
container.


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
Additonally all of the gnmi-gateway instances in the cluster must be able
to reach each other over the network.

It is recommended that you limit the deployment of a cluster to a single
geographic region or a single geographic area with consistent latency for ideal
performance. You may run instances of gnmi-gateway distributed globally but
may encounter performance issues. You'll likely encounter timeout issues
with Zookeeper as your latency begins to approach the Zookeeper `tickTime`.


## Local Development

#### Start Zookeeper for development

This should ony be used for development and not for production. The
container will maintain no state; you will have a completely empty
Zookeeper tree when this starts/restarts. To start zookeeper and expose the
server on `127.0.0.1:2181` run:

```shell script
docker run -d zookeeper
```

#### Test the code

You can test the code by running `make test`.

#### Build the code

You can build the `gnmi-gateway` binary by running `make build`.
