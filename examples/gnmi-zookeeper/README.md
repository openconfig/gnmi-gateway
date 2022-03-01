# gNMI Zookeeper Loader/Prometheus Exporter Example

This is an example of how to setup gnmi-gateway with the Zookeeper Target Loader and the Prometheus Exporter
enabled.

#### Instructions

1.  cd to the root of the gnmi-gateway git repository. You can't build from this directory
    due to build context restrictions imposed by Docker.
2.  Build the gnmi-gateway docker image:
    ```bash
    docker build \
        -f examples/gnmi-zookeeper/Dockerfile \
        -t gnmi-gateway:zkLoader .
    ```
3.  Ensure you have a zookeeper instance accessible to your gnmi-gateway instance. For example:
    ```bash
    docker run \
        --name zookeeper zookeeper
    ```
4.  Set the ZOOKEEPER_HOSTS environment variable to correspond to the socket used by the zookeeper instance:
    ```bash
    export ZOOKEEPER_HOSTS=YOUR_IP:2181
    #(default port is 2181)
    ```
5.  Start the gnmi-gateway docker image:
```bash
    docker run \
        -it --rm \
        -p 59100:59100 \
        --name gnmi-gateway-01 \
        gnmi-gateway:zkLoader \
        ./gnmi-gateway -EnableClustering=false \
        -ZookeeperHosts= ${ZOOKEEPER_HOSTS} \
        -TargetLoaders=zookeeper -EnableGNMIServer \
        -Exporters=prometheus -OpenConfigDirectory=./oc-models/ \
        -ServerTLSCert=server.crt -ServerTLSKey=server.key
```
6.  Add targets and requests to zookeeper. By default, the target configurations will be taken from /targets path and the request configurations from /requests path in zookeeper

    The targets should be stored in zookeeper in yaml format, having the following configuration:
    ```yaml
    demo-gnmi-router:
    addresses:
      - demo-gnmi-router.example.com:9339
    credentials:
      username: myusername
      password: mypassword
    request: demo-request
    meta: {}
    ```
    Also, for the request configuration, they should be stored as follows:
    ```yaml
    target: "*"
    paths:
      - /components
      - /interfaces/interface[name=*]/state/counters
      - /interfaces/interface[name=*]/ethernet/state/counters
      - /interfaces/interface[name=*]/subinterfaces/subinterface[index=*]/state/counters
      - /qos/interfaces/interface[interface-id=*]/output/queues/queue[name=*]/state
      ```

    The request and target keys in the final target configuration will be the zNode names that contain the respective configuration. Therefore, for generating the following gNMI target configuration for the gNMI gateway:
    
    ```yaml
    ---
    connection:
    demo-gnmi-router:
        addresses:
        - demo-gnmi-router.example.com:9339
        credentials:
        username: myusername
        password: mypassword
        request: demo-request
        meta: {}
    request:
    demo-request:
        target: "*"
        paths:
        - /components
        - /interfaces/interface[name=*]/state/counters
        - /interfaces/interface[name=*]/ethernet/state/counters
        - /interfaces/interface[name=*]/subinterfaces/subinterface[index=*]/state/counters
        - /qos/interfaces/interface[interface-id=*]/output/queues/queue[name=*]/state
    ```

    The configuration for "demo-gnmi-router" should be stored as a yaml in zookeeper at /targets/demo-gnmi-router, and the configuration for "demo-request" should be stored in zookeeper at /requests/demo-request

7.  In a new terminal window start the Prometheus docker image: 
    ```bash
    docker run \
        -it --rm \
        -p 9090:9090 \
        -v $(pwd)/examples/gnmi-zookeeper/prometheus.yml:/etc/prometheus/prometheus.yml \
        --name prometheus-01 \
        prom/prometheus
    ```
8.  You should now be able to browse to the Prometheus web server at 
    http://127.0.0.1:9090 and see your gNMI metrics after a few minutes.