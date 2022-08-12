package statsd

import (
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/loaders/zookeeper"
	zkLoader "github.com/openconfig/gnmi-gateway/gateway/loaders/zookeeper"
	"github.com/openconfig/gnmi/ctree"
	pb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/openconfig/gnmi-gateway/gateway/exporters"
)

var _ exporters.Exporter = new(StatsdExporter)
var serverAddress = "127.0.0.1:8125"

var config = &configuration.GatewayConfig{
	Exporters: &configuration.ExportersConfig{
		StatsdHost:       serverAddress,
		GenevaMdmAccount: "testMDM",
		ExtensionArmId:   "testARMID",
	},
	ZookeeperTimeout:          30 * time.Second,
	ExporterMetadataAllowlist: []string{"Account"},
	TargetLoaders: &configuration.TargetLoadersConfig{
		ZookeeperReloadInterval: 10 * time.Second,
	},
	TargetLimit:      100,
	EnableClustering: false,
	Log:              zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.DebugLevel),
}

type MutexStringSlice struct {
	mu sync.Mutex
	s  []string
}

func TestStatsdExporter_Name(t *testing.T) {
	var config configuration.GatewayConfig

	e := NewStatsdExporter(&config)

	assert.Equal(t, "statsd", e.Name())
}

func TestStatsdExporter_Export(t *testing.T) {
	os.Setenv("ZOOKEEPER_HOSTS", "172.17.0.2")
	config.ZookeeperHosts = []string{os.Getenv("ZOOKEEPER_HOSTS")}
	connMgr, err := createZKTargets(t)
	assert.Nil(t, err)

	e := &StatsdExporter{
		config: config,
	}

	assert.Nil(t, err)

	done := make(chan bool, 1)
	output := &MutexStringSlice{
		s: []string{},
	}

	go output.listenUDP(done)
	// TODO: Add mock fluentd receiver

	assert.NotPanics(t, func() {
		err = e.Start(&connMgr)
		assert.Nil(t, err)

		e.Export(ctree.DetachedLeaf(intNotif))
		e.Export(ctree.DetachedLeaf(stringNotif))
	})

	time.Sleep(10 * time.Millisecond)
	done <- true

	go output.testOutput(t)
}

func (output *MutexStringSlice) listenUDP(done chan bool) {
	output.mu.Lock()
	defer output.mu.Unlock()
	statsdServerConn, _ := createStatsdServer()
	buf := make([]byte, 1024)
	for {
		select {
		case <-done:
			output.mu.Unlock()
			return
		default:
			n, _, _ := statsdServerConn.ReadFromUDP(buf)
			output.s = append(output.s, string(buf[0:n]))
		}
	}
}

func (output *MutexStringSlice) testOutput(t assert.TestingT) {
	output.mu.Lock()
	defer output.mu.Unlock()
	assert.Contains(t, output.s, "{\"Account\":\"test\",\"Metric\":\"path0\",\"Namespace\":\"Default\",\"Dims\":{\"origin\":\"b\",\"path\":\"path0\",\"target\":\"test_target0\",\"path0_testKey\":\"testVal\"}}:1|g")
	assert.Contains(t, output.s, "{\"Account\":\"test\",\"Metric\":\"path0\",\"Namespace\":\"Default\",\"Dims\":{\"origin\":\"b\",\"path\":\"path0\",\"target\":\"test_target0\"}}:test|s")
}

func createStatsdServer() (*net.UDPConn, error) {
	StatsdServer, err := net.ListenUDP("udp", &net.UDPAddr{IP: []byte{0, 0, 0, 0}, Port: 8125, Zone: ""})
	if err != nil {
		return nil, err
	}
	return StatsdServer, nil
}

func createZKTargets(t *testing.T) (connections.ConnectionManager, error) {
	zkClient, err := zkLoader.NewZookeeperClient(config.ZookeeperHosts, config.ZookeeperTimeout)
	if err != nil {
		t.Skip("Couldn't connect to zookeeper")
		return nil, err
	}

	if err := zkClient.AddZookeeperData(testTargetConfig); err != nil {
		return nil, err
	}

	var connMgr connections.ConnectionManager

	zkConn, _, err := zk.Connect(config.ZookeeperHosts, config.ZookeeperTimeout)
	if err != nil {
		t.Fatal("Zookeeper connection failed: " + err.Error())
	}

	connMgr, err = connections.NewZookeeperConnectionManagerDefault(config, zkConn, nil)

	if err != nil {
		t.Fatal(err)
	}

	if err := connMgr.Start(); err != nil {
		t.Fatal(err)
	}

	loader := zkLoader.NewZookeeperTargetLoader(config)

	err = loader.Start()
	assert.Nil(t, err)
	go loader.WatchConfiguration(connMgr.TargetControlChan())
	assert.Nil(t, err)

	targetFound := false
	for attempt := 0; attempt < 10; attempt++ {

		_, targetFound = connMgr.(*connections.ZookeeperConnectionManager).GetTargetConfig("test_target0")

		if targetFound {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	assert.True(t, targetFound)

	return connMgr, nil
}

var intNotif = &pb.Notification{
	Prefix: &pb.Path{Target: "test_target0", Origin: "b"},
	Update: []*pb.Update{
		{
			Path: &pb.Path{
				Elem: []*pb.PathElem{
					{
						Name: "path0",
						Key: map[string]string{
							"testKey": "testVal",
						},
					},
					{
						Name: "path1",
					},
				},
			},
			Val: &pb.TypedValue{Value: &pb.TypedValue_IntVal{IntVal: 1}},
		},
	},
	Timestamp: time.Now().UTC().UnixNano(),
}

var stringNotif = &pb.Notification{
	Prefix: &pb.Path{Target: "test_target0", Origin: "b"},
	Update: []*pb.Update{
		{
			Path: &pb.Path{
				Elem: []*pb.PathElem{
					{Name: "path0"},
					{Name: "path1"},
				},
			},
			Val: &pb.TypedValue{Value: &pb.TypedValue_StringVal{StringVal: "test"}},
		},
	},
	Timestamp: time.Now().UTC().UnixNano(),
}

var testTargetConfig = &zookeeper.TargetConfig{
	Connection: map[string]zookeeper.ConnectionConfig{
		"test_target0": {
			Addresses: []string{"172.17.0.4:6030"},
			Meta: map[string]string{
				"NoTLSVerify": "yes",
				"Account":     "test",
			},
			Credentials: zookeeper.CredentialsConfig{
				Username: "admin",
				Password: "admin",
			},
		},
	},
	Request: map[string]zookeeper.RequestConfig{
		"default": {
			Target: "*",
			Paths: []string{
				"/components/component[name=CPU0]/cpu/utilization/state",
			},
		},
		"unused": {
			Target: "*1",
			Paths: []string{
				"/interfaces",
			},
		},
	},
}
