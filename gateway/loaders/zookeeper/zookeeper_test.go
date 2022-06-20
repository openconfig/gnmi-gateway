package zookeeper

import (
	"sort"
	"testing"
	"time"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/loaders"
	"github.com/stretchr/testify/assert"
)

var hosts = []string{"172.17.0.2:2181"}
var zkTimeout = time.Duration(30 * time.Second)

func TestZookeeperLoader(t *testing.T) {
	zkClient := getZKClient(t)
	defer zkClient.conn.Close()

	config := &configuration.GatewayConfig{
		ZookeeperHosts: hosts,
		TargetLoaders: &configuration.TargetLoadersConfig{
			ZookeeperReloadInterval: zkTimeout,
		},
	}

	var loader loaders.TargetLoader
	var connMgr connections.ConnectionManager
	var err error

	connMgr, err = connections.NewZookeeperConnectionManagerDefault(config, zkClient.conn, nil)
	if err != nil {
		t.Fatal("Failed to create zookeeper connection manager")
	}

	assert.NotPanics(t, func() {
		loader = NewZookeeperTargetLoader(config)

		err = loader.Start()
		assert.Nil(t, err)
		go loader.WatchConfiguration(connMgr.TargetControlChan())
		assert.Nil(t, err)
	})
	time.Sleep(500 * time.Millisecond)

	if err := zkClient.AddZookeeperData(testTargetConfig); err != nil {
		assert.Nil(t, err)
		t.Fatal(err)
	}
	t.Log("Successfuly added mock targets for testing ZK loader")

	time.Sleep(500 * time.Millisecond)
	currentConfig, err := loader.GetConfiguration()

	for targetName := range testTargetConfig.Connection {
		assert.Equal(t, targetName, currentConfig.Request[targetName].GetSubscribe().Prefix.Target)

		assert.Equal(t, testTargetConfig.Connection[targetName].Addresses, currentConfig.Target[targetName].GetAddresses())
		assert.Equal(t, testTargetConfig.Connection[targetName].Credentials.Username, currentConfig.Target[targetName].Credentials.Username)
		assert.Equal(t, testTargetConfig.Connection[targetName].Credentials.Password, currentConfig.Target[targetName].Credentials.Password)
		assert.Equal(t, testTargetConfig.Connection[targetName].Meta, currentConfig.Target[targetName].Meta)

		for field := range testTargetConfig.Connection[targetName].Meta {
			assert.Equal(t, testTargetConfig.Connection[targetName].Meta[field], currentConfig.Target[targetName].GetMeta()[field])
		}
	}

	target1Subs := []string{currentConfig.Request["test_target1"].GetSubscribe().Subscription[0].Path.Elem[0].Name}
	target1Subs = append(target1Subs, currentConfig.Request["test_target1"].GetSubscribe().Subscription[1].Path.Elem[0].Name)
	sort.Strings(target1Subs)

	assert.Equal(t, "test_target1", currentConfig.Request["test_target1"].GetSubscribe().Prefix.Target)
	assert.Equal(t, "interfaces", currentConfig.Request["test_target0"].GetSubscribe().GetSubscription()[0].Path.Elem[0].Name)
	assert.Equal(t, []string{"components", "interfaces"}, target1Subs)

	if err := zkClient.Delete(CleanPath(TargetPath) + CleanPath("test_target1")); err != nil {
		assert.Nil(t, err)
		t.Fatal(err)
	}
	t.Log("Successfuly deleted \"test_target1\"")

	if err := zkClient.AddZookeeperData(modifiedTarget0); err != nil {
		assert.Nil(t, err)
		t.Fatal(err)
	}

	if err := CreateFullPath(zkClient.conn, "/full/path/creation", zkClient.acl); err != nil {
		assert.Nil(t, err)
		t.Fatal(err)
	}
	zkClient.Delete("/full/path/creation")
	zkClient.Delete("/full/path")
	zkClient.Delete("/full")

	if err := zkClient.createNode("/test/node/creation"); err != nil {
		assert.Nil(t, err)
		t.Fatal(err)
	}
	zkClient.Delete("/test/node/creation")
	zkClient.Delete("/test/node")
	zkClient.Delete("/test")

	zkClient.Delete(CleanPath(TargetPath) + CleanPath("test_target0"))
	t.Log("Successfuly deleted mock targets in zookeeper")

	time.Sleep(500 * time.Millisecond)
	loader.(*ZookeeperTargetLoader).Close()
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
		return nil
	}

	return zkClient
}

var testTargetConfig = &TargetConfig{
	Connection: map[string]ConnectionConfig{
		"test_target0": {
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
		"test_target1": {
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
				"/interfaces",
			},
		},
		"unused": {
			Target: "*1",
			Paths: []string{
				"/components",
			},
		},
	},
}

var modifiedTarget0 = &TargetConfig{
	Connection: map[string]ConnectionConfig{
		"test_target0": {
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
	},
}
