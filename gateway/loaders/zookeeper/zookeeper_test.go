package zookeeper

import (
	"testing"
	"time"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/loaders"
	"github.com/stretchr/testify/assert"
	// "github.com/openconfig/gnmi-gateway/gateway/configuration"
)

var hosts = []string{"172.17.0.2:2181"}
var zkTimeout = time.Duration(30 * time.Second)

func TestZookeeperLoader(t *testing.T) {
	zkClient := getZKClient(t)
	defer zkClient.conn.Close()

	assert.NotPanics(t, func() {
		err := zkClient.Wipe()

		assert.Nil(t, err)
		t.Log("Successfuly wiped existing ZK data")
	})

	config := &configuration.GatewayConfig{
		ZookeeperHosts: hosts,
		TargetLoaders: &configuration.TargetLoadersConfig{
			ZookeeperReloadInterval: zkTimeout,
		},
	}

	var loader loaders.TargetLoader

	assert.NotPanics(t, func() {
		loader = NewZookeeperTargetLoader(config)
		err := loader.Start()
		assert.Nil(t, err)
	})

	if err := zkClient.AddZookeeperData(testTargetConfig); err != nil {
		assert.Nil(t, err)
		t.Fatal(err)
	}
	t.Log("Successfuly added mock targets for testing ZK loader")

	defer zkClient.DeleteZookeeperData(testTargetConfig)
	t.Log("Successfuly deleted mock targets in zookeeper")
}

func getZKClient(t *testing.T) *ZookeeperClient {
	zkClient, err := NewZookeeperClient(hosts, zkTimeout)

	assert.Nil(t, err)
	for i := 0; i < 5; i++ {
		if zkClient.conn.State().String() == "StateConnected" {
			t.Log(i)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	connState := zkClient.conn.State().String()

	if connState != "StateConnected" && connState != "StateHasSession" {
		t.Log(zkClient.conn.State().String())
		t.Log("Zookeeper failed to connect to hosts: ", hosts)
		t.Skip()
	}

	return zkClient
}

var testTargetConfig = &TargetConfig{
	Connection: map[string]ConnectionConfig{
		"target0": {
			Addresses: []string{"mockIP0"},
			Meta: map[string]string{
				"NoTLSVerify": "yes",
				"Account":     "test",
			},
			Credentials: CredentialsConfig{
				Username: "admin",
				Password: "admin",
			},
		},
		"target1": {
			Addresses: []string{"mockIP1"},
			Meta: map[string]string{
				"NoTLSVerify": "yes",
				"Account":     "test",
			},
			Credentials: CredentialsConfig{
				Username: "admin",
				Password: "admin",
			},
		},
	},
	Request: map[string]RequestConfig{
		"default": {
			Target: "*",
			Paths: []string{
				"/components",
				"/interfaces",
			},
		},
	},
}
