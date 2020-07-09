# gNMI Prometheus Exporter Example

This is an example of how to setup gnmi-gateway with the Prometheus Exporter
enabled.

#### Instructions

1.  Copy `targets-example.json` from the project root to
    `example/gnmi-prometheus/targets.json` and modify `targets.json` to match
    the details of the gNMI target you want to connect to (your router).
2.  cd to the root of the gnmi-gateway git repository. You can't build from this directory
    due to build context restrictions imposed by Docker.
3.  Build the gnmi-gateway docker image:
    ```bash
    docker build \
        -f examples/gnmi-prometheus/Dockerfile \
        -t gnmi-gateway:latest .
    ```
4. Create the network bridge that will be used to connect the two docker containers:
    ```bash
    docker network create --driver bridge gnmi-net
    ```
4.  Start the gnmi-gateway docker image:
    ```bash
    docker run \
        -it --rm \
        -p 59100:59100 \
        -v $(pwd)/examples/gnmi-prometheus/targets.json:/opt/gnmi-gateway/targets.json \
        --name gnmi-gateway-01 \
        --network gnmi-net \
        gnmi-gateway:latest
    ```
5.  In a new terminal window start the Prometheus docker image: 
    ```bash
    docker run \
        -it --rm \
        -p 9090:9090 \
        -v $(pwd)/examples/gnmi-prometheus/prometheus.yml:/etc/prometheus/prometheus.yml \
        --name prometheus-01 \
        --network gnmi-net \
        prom/prometheus
    ```
6.  You should now be able to browse to the Prometheus web server at 
    http://127.0.0.1:9090 and see your gNMI metrics after a few minutes.
