package statsd

import (
	"testing"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi/ctree"
	pb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/proto/target"
	"github.com/stretchr/testify/assert"

	"github.com/openconfig/gnmi-gateway/gateway/exporters"
)

var _ exporters.Exporter = new(StatsdExporter)
var serverAddress = "127.0.0.1:8125"

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

func TestStatsdExporter_Name(t *testing.T) {
	var config configuration.GatewayConfig

	e := NewStatsdExporter(&config)

	assert.Equal(t, "statsd", e.Name())
}

func TestStatsdExporter_Export(t *testing.T) {

	config := &configuration.GatewayConfig{
		Exporters: &configuration.ExportersConfig{
			StatsdHost: serverAddress,
		},
	}
	config.ExporterMetadataAllowlist = []string{"Account"}

	var connMgr connections.ConnectionManager

	zkConn, _, err := zk.Connect(config.ZookeeperHosts, config.ZookeeperTimeout)

	connMgr, err = connections.NewZookeeperConnectionManagerDefault(config, zkConn, nil)

	if err != nil {
		panic(err)
	}

	if err := connMgr.Start(); err != nil {
		panic(err)
	}

	mockTarget := &target.Target{
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
	mockRequest := &pb.SubscribeRequest{
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
	targetChan := connMgr.TargetControlChan()
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

	e := &StatsdExporter{
		config: config,
	}
	if err := e.Start(&connMgr); err != nil {
		panic(err)
	}

	targetChan <- controlMsg

	targetFound := false
	for attempt := 0; attempt < 10; attempt++ {
		_, targetFound = connMgr.GetTargetConfig("a")
		if targetFound {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	assert.True(t, targetFound)
	assert.NotPanics(t, func() {
		e.Export(ctree.DetachedLeaf(intNotif))

		e.Export(ctree.DetachedLeaf(stringNotif))
	})

}
