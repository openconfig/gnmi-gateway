package statsd

import (
	"net"
	"testing"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/loaders/zookeeper"
	"github.com/openconfig/gnmi/ctree"
	pb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/proto/target"
	"github.com/stretchr/testify/assert"

	"github.com/openconfig/gnmi-gateway/gateway/exporters"
)

var _ exporters.Exporter = new(StatsdExporter)
var serverAddress = "127.0.0.1:8125"
var zookeeperAddress = "172.17.0.2:2181"

var config = &configuration.GatewayConfig{
	Exporters: &configuration.ExportersConfig{
		StatsdHost: serverAddress,
	},
	ZookeeperHosts:            []string{zookeeperAddress},
	ExporterMetadataAllowlist: []string{"Account"},
}

func TestStatsdExporter_Name(t *testing.T) {
	var config configuration.GatewayConfig

	e := NewStatsdExporter(&config)

	assert.Equal(t, "statsd", e.Name())
}

func TestStatsdExporter_Export(t *testing.T) {
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

	e := &StatsdExporter{
		config: config,
	}

	targetFound := false
	for attempt := 0; attempt < 10; attempt++ {
		_, targetFound = connMgr.GetTargetConfig("a")
		if targetFound {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	assert.True(t, targetFound)

	statsdServerConn, err := createStatsdServer()

	assert.Nil(t, err)

	go testStatsdOutput(t, statsdServerConn)

	assert.NotPanics(t, func() {
		e.Export(ctree.DetachedLeaf(intNotif))

		e.Export(ctree.DetachedLeaf(stringNotif))
	})

}

func createStatsdServer() (*net.UDPConn, error) {
	StatsdServer, err := net.ListenUDP("udp", &net.UDPAddr{IP: []byte{0, 0, 0, 0}, Port: 8125, Zone: ""})
	if err != nil {
		return nil, err
	}
	return StatsdServer, nil
}

func testStatsdOutput(t *testing.T, serverConn *net.UDPConn) {
	t.Log("hello")
	buf := make([]byte, 1024)
	for {
		n, addr, _ := serverConn.ReadFromUDP(buf)
		t.Log("Received ", string(buf[0:n]), " from ", addr)
	}
}

func createZKTargets(connMgr *connections.ConnectionManager) {
	zkClient, err := zookeeper.NewZookeeperClient(config.ZookeeperHosts, config.ZookeeperTimeout)
	if err != nil {
		//TODO Do something
	}

	// zkClient.AddZookeeperData()
	targetChan := (*connMgr).TargetControlChan()
	targetMap := make(map[string]*target.Target)
	targetMap["a"] = mockTarget
	requestMap := make(map[string]*pb.SubscribeRequest)
	requestMap["default"] = mockRequest

	controlMsg := new(connections.TargetConnectionControl)
	targetConfig := &target.Configuration{
		Target:  targetMap,
		Request: requestMap,
	}

	controlMsg.Insert = targetConfig
	targetChan <- controlMsg
}

var intNotif = &pb.Notification{
	Prefix: &pb.Path{Target: "a", Origin: "b"},
	Update: []*pb.Update{
		{
			Path: &pb.Path{
				Elem: []*pb.PathElem{
					{
						Name: "test",
						Key: map[string]string{
							"testKey": "testVal",
						},
					},
				},
			},
			Val: &pb.TypedValue{Value: &pb.TypedValue_IntVal{IntVal: 1}},
		},
	},
	Timestamp: time.Now().UTC().UnixNano(),
}

var stringNotif = &pb.Notification{
	Prefix: &pb.Path{Target: "a", Origin: "b"},
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

var mockTarget = &target.Target{
	Addresses: []string{"127.0.0.1"},
	Credentials: &target.Credentials{
		Username: "admin",
		Password: "admin",
	},
	Request: "default",
	Meta: map[string]string{
		"Account": "AFONFA",
	},
}

var mockRequest = &pb.SubscribeRequest{
	Request: &pb.SubscribeRequest_Subscribe{
		Subscribe: &pb.SubscriptionList{
			Prefix: intNotif.GetPrefix(),
			Subscription: []*pb.Subscription{
				{
					Path: intNotif.GetPrefix(),
				},
			},
		},
	},
}
