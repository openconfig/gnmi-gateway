package main

import (
	"fmt"
	"os"
	"time"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi/ctree"
	pb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog"

	"github.com/openconfig/gnmi-gateway/gateway/benchmarks/statsd_server"
	"github.com/openconfig/gnmi-gateway/gateway/exporters/statsd"
)

const (
	serverAddress = "127.0.0.1"
	serverPort = 8125

	expNotifications = 150000
	// how long to wait for all the notifications to be received on the statsd
	// side.
	testTimeoutS = 15
)

var (
	config = &configuration.GatewayConfig{
		Exporters: &configuration.ExportersConfig{
			StatsdHost: fmt.Sprintf("%s:%d", serverAddress, serverPort),
		},
		ZookeeperTimeout:          30 * time.Second,
		ExporterMetadataAllowlist: []string{"Account"},
		TargetLimit:      100,
		EnableClustering: false,
		Log:              zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.InfoLevel),
	}
)

func getNotification() *pb.Notification {
	return &pb.Notification{
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
}


func main() {
	server, err := statsd_server.NewServer(
		serverAddress, serverPort, expNotifications, config.Log)
	if err != nil {
		panic(err)
	}

	if err := server.Start(); err != nil {
		panic(err)
	}

	e := statsd.NewStatsdExporter(config)	
	err = e.Start(nil)
	if err != nil {
		panic(err)
	}

	tstart := time.Now()
	for i := 0; i < expNotifications; i+=1 {
		e.Export(ctree.DetachedLeaf(getNotification()))
		if i % 10000 == 0 {
			config.Log.Info().Msgf("submitted notifications: %d", i + 1)
		}
	}
	tend := time.Now()
	config.Log.Info().Msgf("Submitted %d notifications in %s", expNotifications, tend.Sub(tstart))

	done := make(chan struct{})
	go func() {
		// Wait for the server to receive all notifications
		server.Wait()
		close(done)
	}()

	select {
	case <-done:
		config.Log.Info().Msg("benchmark completed successfully")
		tend = time.Now()
		config.Log.Info().Msgf("total test duration: %s", tend.Sub(tstart))
	case <-time.After(time.Duration(testTimeoutS) * time.Second):
		config.Log.Info().Msg("statsd timeout while waiting for notifications")
	}

	server.PrintStats()
}
