# gNMI Gateway

A distributed and highly available service for subscribing to multiple gNMI targets.

### Local Development

##### Start Zookeeper for development

This will start zookeeper and expose the server on 127.0.0.1:2181

```shell script
docker run --restart always -d zookeeper
```

