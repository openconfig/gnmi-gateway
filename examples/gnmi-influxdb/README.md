# gNMI InfluxDB Exporter Example

This is an example of how to setup gnmi-gateway with the InfluxDB Exporter
enabled.

### Requirements

- docker-compose (Note: [Compose CLI](https://docs.docker.com/compose/cli-command/) is in tech preview and removes the docker-compose requirement)

### Gotcha's

- Make sure the time is syncronized between the target routers, influxdb and the gnmi-gateway
- SSL/TLS __must__ be enabled (For example, on an Arista/EOS device a [self signed cert](https://gist.github.com/mathershifter/f537a46e220a5f194e2bccf3f0d61a70) will be enough)

### Instructions

1.  Copy `targets-example.json` from the project root to
    `example/gnmi-influxdb/targets.json` and modify `targets.json` to match
    the details of the gNMI target(s) you want to connect to (your router(s)).

2.  cd to examples/gnmi-influxdb

3. Export ENV variables
    ```bash
    export INFLUXDB_TOKEN=
    export INFLUXDB_ORG=myorg
    export INFLUXDB_BUCKET=telemetry
    export INFLUXDB_TARGET=http://influxdb:8086
    ```

4.  Build the gnmi-gateway docker image:
    ```bash
    docker-compose build --no-cache gnmi-gateway
    ```

5.  Start InfluxDB
    ```bash
    docker-compose up -d influxdb
    ```

6.  Setup InfluxDB
    ```bash
    # only keep an hour of data for the example...
    docker-compose exec influxdb influx setup \
        -o ${INFLUXDB_ORG} \
        -b ${INFLUXDB_BUCKET} \
        -u admin -p p4ssw0rd \
        -r 1h -f
    ```

7. Set authentication token environment variable
    ```bash
    export INFLUXDB_TOKEN=`docker-compose exec -T influxdb influx auth list --hide-headers | awk '{print $4}'`

    # or use `jq`

    export INFLUXDB_TOKEN=`docker-compose exec -T influxdb influx auth list --hide-headers --json | jq -r ".[].token"`
    ```

8.  In a new terminal, start the gnmi-gateway docker image:
    ```bash
    docker-compose up gnmi-gateway
    ```

9.  You should now be able to browse to the InfluxDB web server at 
    http://127.0.0.1:8086 and see your gNMI metrics after a few minutes.


###  Example Query

Navigate to "Explore" and select "Script Builder" 

```bash
import "experimental/aggregate"

from(bucket: "telemetry")
|> range(start: v.timeRangeStart, stop: v.timeRangeStop)
|> filter(fn: (r) => r["_measurement"] == "interfaces_interface_state_counters")
|> filter(fn: (r) => r["_field"] == "in_octets" or r["_field"] == "out_octets")
|> aggregate.rate(every: 1m, unit: 1s, groupColumns: ["name", "target", "_field"])
|> map(fn: (r) => ({
    r with
    _value: r._value * 8.0
    })
)
```