// Copyright 2020 Netflix Inc
// Author: Colin McIntosh (colin@netflix.com)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Copyright 2018 Google Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Portions of this file including StartGNMIServer (excluding modifications) are from
// https://github.com/openconfig/gnmi/blob/89b2bf29312cda887da916d0f3a32c1624b7935f/cmd/gnmi_collector/gnmi_collector.go

// Package gateway provides an easily configurable server that can connect to multiple gNMI targets (devices) and
// relay received messages to various exporters and to downstream gNMI clients.
//
// Targets are configured via a TargetLoader which generate all of the needed configuration for connecting to a target.
// See github.com/openconfig/gnmi/proto/target for details on the Configuration options that are available.
// See the TargetLoader docs for the minimum configuration required to connect to a target.
//
// The gateway only supports TLS so you'll need to generate some keys first if you don't already have them.
// In production you should use properly signed TLS certificates.
//		# Generate private key (server.key)
//		openssl genrsa -out server.key 2048
//		# or
//		openssl ecparam -genkey -name secp384r1 -out server.key
//
//		# Generation of self-signed(x509) public key (server.crt) based on the private key (server.key)
//		openssl req -new -x509 -sha256 -key server.key -out server.crt -days 3650
//
// You'll also need a copy of the latest OpenConfig YANG models if you don't already have it.
//		git clone https://github.com/openconfig/public.git oc-models
//
// Finally, you need to build your target configurations. Copy targets-example.json to targets.json and edit it to
// match the targets you want to connect to.
//		cp targets-example.json targets.json
//		vim targets.json
//
// See the example below or the Main() function in gateway.go for an example of how to start the server.
// If you'd like to just use the built-in loaders and exporters you can configure them more easily from the command line:
// 		go build
//		./gnmi-gateway -EnableGNMIServer -EnablePrometheusExporter -OpenConfigDirectory=./oc-models/
package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	_ "net/http/pprof"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Netflix/spectator-go"
	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi/ctree"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"

	"github.com/openconfig/gnmi-gateway/gateway/clustering"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/loaders"
	"github.com/openconfig/gnmi-gateway/gateway/loaders/cluster"
	"github.com/openconfig/gnmi-gateway/gateway/server"
	"github.com/openconfig/gnmi-gateway/gateway/stats"
)

var (
	// Buildtime is set to the current time during the build process by GOLDFLAGS
	Buildtime string
	// Version is set to the current git tag during the build process by GOLDFLAGS
	Version string
)

var (
	CPUProfile   string
	PrintVersion bool
	PProf        bool
)

type Gateway struct {
	clientLock       sync.Mutex
	clients          []*CacheClient
	cluster          clustering.ClusterMember
	config           *configuration.GatewayConfig
	connMgr          connections.ConnectionManager
	zkConn           *zk.Conn
	zkEventListeners []chan<- zk.Event
}

type CacheClient struct {
	buffer      chan *ctree.Leaf
	bufferGauge *spectator.Gauge
	name        string
	send        func(leaf *ctree.Leaf)
	External    bool
}

// NewCacheClient creates a new cache client instance and starts the associated
// goroutines.
func NewCacheClient(name string, newClient func(leaf *ctree.Leaf), external bool, size uint64) *CacheClient {
	metricTags := map[string]string{
		"gnmigateway.transition_buffer_name": name,
	}
	c := &CacheClient{
		buffer:      make(chan *ctree.Leaf, size),
		bufferGauge: stats.Registry.Gauge("gnmigateway.transition_buffer_size", metricTags),
		name:        name,
		send:        newClient,
		External:    external,
	}
	go c.run()
	go c.metrics()
	return c
}

func (c *CacheClient) metrics() {
	for {
		time.Sleep(30 * time.Second)
		c.bufferGauge.Set(float64(len(c.buffer)))
	}
}

func (c *CacheClient) run() {
	for l := range c.buffer {
		c.send(l)
	}
}

func (c *CacheClient) Send(leaf *ctree.Leaf) {
	c.buffer <- leaf
}

// StartOpts is passed to StartGateway() and is used to set the running configuration
type StartOpts struct {
	// Loader for targets
	TargetLoaders []loaders.TargetLoader
	// Exporters to run
	Exporters []exporters.Exporter
}

// NewGateway returns an new Gateway instance.
func NewGateway(config *configuration.GatewayConfig) *Gateway {
	return &Gateway{
		clients: []*CacheClient{},
		config:  config,
	}
}

// Client functions need to complete very quickly to prevent blocking upstream.
func (g *Gateway) AddClient(name string, newClient func(leaf *ctree.Leaf), external bool) {
	g.clientLock.Lock()
	defer g.clientLock.Unlock()
	g.clients = append(g.clients, NewCacheClient(name, newClient, external, g.config.GatewayTransitionBufferSize))
}

// StartGateway starts up all of the loaders and exporters provided by StartOpts. This is the
// primary way the server should be started.
func (g *Gateway) StartGateway(opts *StartOpts) error {
	g.config.Log.Info().Msg("Starting GNMI Gateway.")
	stats.Registry.Counter("gnmigateway.starting", stats.NoTags).Increment()

	if (g.config.StatsSpectatorConfig != nil && g.config.StatsSpectatorConfig.Uri != "") ||
		g.config.StatsSpectatorURI != "" {
		_, err := stats.StartSpectator(g.config)
		if err != nil {
			return fmt.Errorf("unable to start Spectator: %v", err)
		}
	}

	// The finished channel to listens for errors from child goroutines.
	// The first error will cause this function to return.
	finished := make(chan error, 1)

	var err error
	var clusterMember string
	if (g.config.EnableClustering == true) && (g.config.ZookeeperHosts != nil && len(g.config.ZookeeperHosts) > 0) {
		if !g.config.EnableGNMIServer {
			return errors.New("gNMI server is required for clustering: Set -EnableGNMIServer or disable clustering by removing -ZookeeperHosts")
		}

		if g.config.ServerAddress == "" {
			g.config.ServerAddress, err = getLocalIP()
			if err != nil {
				return fmt.Errorf("clustering requires ServerAddress to be set: %v", err)
			}
		}

		if g.config.ServerPort == 0 {
			g.config.ServerPort = g.config.ServerListenPort
		}

		clusterMember = g.config.ServerAddress + ":" + strconv.Itoa(g.config.ServerPort)

		g.zkConn, err = g.ConnectToZookeeper()
		if err != nil {
			g.config.Log.Error().Msgf("Unable to connect to Zookeeper: %v", err)
			return err
		}
		g.cluster = clustering.NewZookeeperClusterMember(g.config, g.zkConn, clusterMember)
		g.config.Log.Info().Msg("Clustering is enabled.")
	} else {
		g.config.Log.Info().Msg("Clustering is NOT enabled. No locking or cluster coordination will happen.")
	}

	connZKEventChan := make(chan zk.Event, 1)
	g.zkEventListeners = append(g.zkEventListeners, connZKEventChan)
	g.connMgr, err = connections.NewZookeeperConnectionManagerDefault(g.config, g.zkConn, connZKEventChan)
	if err != nil {
		g.config.Log.Error().Msgf("Unable to create connection manager: %v", err)
		os.Exit(1)
	}
	g.config.Log.Info().Msg("Starting connection manager.")
	if err := g.connMgr.Start(); err != nil {
		g.config.Log.Error().Msgf("Unable to start connection manager: %v", err)
		return err
	}
	g.connMgr.Cache().SetClient(g.sendUpdateToClients)

	if g.config.EnableGNMIServer {
		if g.config.ServerListenAddress == "" {
			return fmt.Errorf("ServerListenAddress can't be empty with -EnableGNMIServer")
		}

		if g.config.ServerListenPort == 0 {
			return fmt.Errorf("ServerListenPort can't be empty with -EnableGNMIServer")
		}

		g.config.Log.Info().Msgf("Starting gNMI server on 0.0.0.0:%d.", g.config.ServerListenPort)
		go func() {
			stats.Registry.Counter("gnmigateway.server.started", stats.NoTags).Increment()
			if err := g.StartGNMIServer(); err != nil {
				g.config.Log.Error().Msgf("Unable to start gNMI server: %v", err)
				finished <- err
			}
		}()
	}

	if g.cluster != nil {
		go func() {
			// TODO: check the gNMI server goroutine is serving before registering. Other cluster members may try to
			//       connect before the gNMI server is up if we register too early.
			err := g.cluster.Register()
			if err != nil {
				finished <- fmt.Errorf("unable to register this cluster member '%s': %v", clusterMember, err)
			} else {
				g.config.Log.Info().Msgf("Registered this cluster member as '%s'", clusterMember)
			}
		}()
		opts.TargetLoaders = append(opts.TargetLoaders, cluster.NewClusterTargetLoader(g.config, g.cluster))
	}

	for _, name := range g.config.TargetLoaders.Enabled {
		loader := loaders.New(name, g.config)
		if loader == nil {
			return fmt.Errorf("no registered target loader: '%s'", name)
		}
		opts.TargetLoaders = append(opts.TargetLoaders, loader)
	}

	for _, name := range g.config.Exporters.Enabled {
		exporter := exporters.New(name, g.config)
		if exporter == nil {
			return fmt.Errorf("no registered exporter: '%s'", name)
		}
		opts.Exporters = append(opts.Exporters, exporter)
	}

	for _, loader := range opts.TargetLoaders {
		go func(loader loaders.TargetLoader) {
			err := loader.Start()
			if err != nil {
				for err == zk.ErrNoServer {
					g.config.Log.Error().Msgf("Zookeeper couldn't connect: %v ; Retrying...", err)
					time.Sleep(10 * time.Second)
					err = loader.Start()
				}
				g.config.Log.Error().Msgf("Unable to start target loader %T: %v", loader, err)
				finished <- err
			}
			stats.Registry.Counter("gnmigateway.loaders.started", stats.NoTags).Increment()
			err = loader.WatchConfiguration(g.connMgr.TargetControlChan())
			if err != nil {
				finished <- fmt.Errorf("error during target loader %T watch: %v", loader, err)
			}
		}(loader)
	}

	for _, exporter := range opts.Exporters {
		go func(exporter exporters.Exporter) {
			err := exporter.Start(&g.connMgr)
			if err != nil {
				err = fmt.Errorf("unable to start exporter '%s': %v", exporter.Name(), err)
				g.config.Log.Error().Msg(err.Error())
				finished <- err
			}
			g.AddClient(exporter.Name(), exporter.Export, true)
			stats.Registry.Counter("gnmigateway.exporters.started", stats.NoTags).Increment()
		}(exporter)
	}

	stats.Registry.Counter("gnmigateway.started", stats.NoTags).Increment()
	err = <-finished
	stats.Registry.Counter("gnmigateway.stopped", stats.NoTags).Increment()
	return err
}

func (g *Gateway) sendUpdateToClients(leaf *ctree.Leaf) {
	for _, client := range g.clients {
		if client.External {
			notification := leaf.Value().(*gnmi.Notification)
			target := notification.GetPrefix().GetTarget()
			if g.connMgr.Forwardable(target) {
				client.Send(leaf)
			}
		} else {
			client.Send(leaf)
		}
	}
}

// SendNotificationsToClients forwards gNMI notifications to gateway clients
// (exporters and the gNMI cache + server) as a detached leaf to bypass
// the cache.
//func (g *Gateway) SendNotificationToClients(n *gnmi.Notification) {
//	g.sendUpdateToClients(ctree.DetachedLeaf(n))
//}

// StartGNMIServer will start the gNMI server that serves the Subscribe
// interface to downstream gNMI clients.
func (g *Gateway) StartGNMIServer() error {
	if g.config.ServerTLSCreds == nil {
		if g.config.ServerTLSCert == "" || g.config.ServerTLSKey == "" {
			return fmt.Errorf("no TLS creds: you must specify a ServerTLSCert and ServerTLSKey")
		}

		// Initialize TLS credentials.
		creds, err := credentials.NewServerTLSFromFile(g.config.ServerTLSCert, g.config.ServerTLSKey)
		if err != nil {
			return fmt.Errorf("failed to generate credentials: %v", err)
		}
		g.config.ServerTLSCreds = creds
	}

	// Create a grpc Server.
	srv := grpc.NewServer(grpc.Creds(g.config.ServerTLSCreds))
	reflection.Register(srv)
	// Initialize gNMI Proxy Subscribe server.
	gnmiServerOpts := &server.GNMIServerOpts{
		Config:  g.config,
		Cache:   g.connMgr.Cache(),
		Cluster: g.cluster,
		ConnMgr: g.connMgr,
	}
	subscribeSrv, err := server.NewServer(gnmiServerOpts)
	if err != nil {
		return fmt.Errorf("Could not instantiate gNMI server: %v", err)
	}
	gnmi.RegisterGNMIServer(srv, subscribeSrv)
	// Forward streaming updates to clients.
	g.AddClient("gnmi_server", subscribeSrv.Update, false)
	// Register listening port and start serving.
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", g.config.ServerListenPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	go func() {
		err := srv.Serve(lis) // blocks
		g.config.Log.Error().Msgf("Error running gNMI server: %v", err)
	}()
	defer srv.Stop()
	ctx := context.Background()
	<-ctx.Done()
	return ctx.Err()
}

type ZKLogger struct {
	log zerolog.Logger
}

func (z *ZKLogger) Printf(a string, b ...interface{}) {
	z.log.Info().Msgf("Zookeeper: %s", fmt.Sprintf(a, b...))
}

func (g *Gateway) ConnectToZookeeper() (*zk.Conn, error) {
	zkLogger := &ZKLogger{
		log: g.config.Log,
	}

	g.config.Log.Info().Msgf("Connecting to Zookeeper hosts: %v.", strings.Join(g.config.ZookeeperHosts, ", "))
	newConn, eventChan, err := zk.Connect(g.config.ZookeeperHosts, g.config.ZookeeperTimeout, zk.WithLogger(zkLogger))
	if err != nil {
		g.config.Log.Error().Msgf("Unable to connect to Zookeeper: %v", err)
		return nil, err
	}
	go g.zookeeperEventHandler(eventChan)
	g.config.Log.Info().Msg("Zookeeper connected.")
	return newConn, nil
}

func (g *Gateway) zookeeperEventHandler(zkEventChan <-chan zk.Event) {
	var disconnectedCount int
	for event := range zkEventChan {
		g.config.Log.Info().Msgf("Zookeeper State: %s", event.State.String())
		switch event.State {
		case zk.StateDisconnected:
			disconnectedCount++
			if disconnectedCount > 50 {
				panic("too many Zookeeper disconnects")
			}
		case zk.StateHasSession:
			disconnectedCount = 0
		}
		for _, eventChan := range g.zkEventListeners {
			eventChan <- event
		}
	}
}

// Get the first non-loopback IP address from the local system.
func getLocalIP() (string, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return "", fmt.Errorf("unable to get local interface addresses: %v", err)
	}

	var validIP []string
	for _, ip := range addresses {
		ipNet, ok := ip.(*net.IPNet)
		if !ok {
			continue
		}

		if ipNet.IP.IsLoopback() {
			continue
		}

		if ipNet.IP.To4() != nil || ipNet.IP.To16() != nil {
			validIP = append(validIP, ipNet.IP.String())
		}
	}

	if validIP == nil || len(validIP) < 1 {
		return "", fmt.Errorf("no valid local IPs found")
	}

	sort.Strings(validIP)
	return validIP[0], nil
}
