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
//		./gnmi-gateway -EnableServer -EnablePrometheus -OpenConfigDirectory=./oc-models/
package gateway

import (
	"context"
	"errors"
	"fmt"
	"github.com/openconfig/gnmi/ctree"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/samuel/go-zookeeper/zk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	_ "net/http/pprof"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/clustering"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/connections"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/server"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"strconv"
	"strings"
	"sync"
)

var (
	// Buildtime is set to the current time during the build process by GOLDFLAGS
	Buildtime string
	// Version is set to the current git tag during the build process by GOLDFLAGS
	Version string
)

var (
	CPUProfile       string
	EnablePrometheus bool
	LogCaller        bool
	PrintVersion     bool
	PProf            bool
)

type Gateway struct {
	clientLock sync.Mutex
	clients    []func(leaf *ctree.Leaf)
	cluster    clustering.Cluster
	config     *configuration.GatewayConfig
	zkConn     *zk.Conn
}

// StartOpts is passed to StartGateway() and is used to set the running configuration
type StartOpts struct {
	// Loader for targets
	TargetLoaders []targets.TargetLoader
	// Exporters to run
	Exporters []exporters.Exporter
}

func NewGateway(config *configuration.GatewayConfig) *Gateway {
	return &Gateway{
		clients: []func(leaf *ctree.Leaf){},
		config:  config,
	}
}

// Client functions need to complete very quickly to prevent blocking upstream.
func (g *Gateway) AddClient(newClient func(leaf *ctree.Leaf)) {
	g.clientLock.Lock()
	defer g.clientLock.Unlock()
	g.clients = append(g.clients, newClient)
}

// StartGateway starts up all of the loaders and exporters provided by StartOpts. This is the
// primary way the server should be started.
func (g *Gateway) StartGateway(opts *StartOpts) error {
	g.config.Log.Info().Msg("Starting GNMI Gateway.")

	// The finished channel to listens for errors from child goroutines.
	// The first error will cause this function to return.
	finished := make(chan error, 1)

	var err error
	if g.config.ZookeeperHosts != nil && len(g.config.ZookeeperHosts) > 0 {
		if !g.config.EnableServer {
			return errors.New("gNMI server is required for clustering: Set -EnableServer or disable clustering by removing -ZookeeperHosts")
		}

		g.zkConn, err = g.ConnectToZookeeper()
		if err != nil {
			g.config.Log.Error().Msgf("Unable to connect to Zookeeper: %v", err)
			return err
		}
		g.cluster = clustering.NewZookeeperCluster(g.config, g.zkConn)
		g.config.Log.Info().Msg("Clustering is enabled.")
	} else {
		g.config.Log.Info().Msg("Clustering is NOT enabled. No locking or cluster coordination will happen.")
	}

	connMgr, err := connections.NewConnectionManagerDefault(g.config, g.zkConn)
	if err != nil {
		g.config.Log.Error().Err(err).Msg("Unable to create connection manager.")
		os.Exit(1)
	}
	g.config.Log.Info().Msg("Starting connection manager.")
	if err := connMgr.Start(); err != nil {
		g.config.Log.Error().Err(err).Msgf("Unable to start connection manager: %v", err)
		return err
	}
	connMgr.Cache().SetClient(g.sendUpdateToClients)

	if g.config.EnableServer {
		g.config.Log.Info().Msgf("Starting gNMI server on 0.0.0.0:%d.", g.config.ServerListenPort)
		go func() {
			if err := g.StartGNMIServer(connMgr); err != nil {
				g.config.Log.Error().Err(err).Msg("Unable to start gNMI server.")
				finished <- err
			}
		}()
	}

	if g.cluster != nil {
		go func() {
			// TODO: check the gNMI server goroutine is serving before registering. Other cluster members may try to
			//       connect before the gNMI server is up if we register too early.
			clusterMember := g.config.ServerAddress + ":" + strconv.Itoa(g.config.ServerPort)
			err := g.cluster.Register(clusterMember)
			if err != nil {
				finished <- fmt.Errorf("unable to register this cluster member '%s': %v", clusterMember, err)
			} else {
				g.config.Log.Info().Msgf("Registered this cluster member as '%s'", clusterMember)
			}
		}()
		opts.TargetLoaders = append(opts.TargetLoaders, targets.NewClusterTargetLoader(g.config, g.cluster))
	}

	if g.config.TargetJSONFile != "" {
		opts.TargetLoaders = append(opts.TargetLoaders, targets.NewJSONFileTargetLoader(g.config))
	}

	for _, loader := range opts.TargetLoaders {
		go func(loader targets.TargetLoader) {
			err := loader.Start()
			if err != nil {
				g.config.Log.Error().Err(err).Msgf("Unable to start target loader %T", loader)
				finished <- err
			}
			loader.WatchConfiguration(connMgr.TargetConfigChan())
		}(loader)
	}

	for _, exporter := range opts.Exporters {
		go func(exporter exporters.Exporter) {
			err := exporter.Start(connMgr.Cache())
			if err != nil {
				g.config.Log.Error().Err(err).Msgf("Unable to start exporter %T", exporter)
				finished <- err
			}
			g.AddClient(exporter.Export)
		}(exporter)
	}

	return <-finished
}

func (g *Gateway) sendUpdateToClients(leaf *ctree.Leaf) {
	for _, client := range g.clients {
		client(leaf)
	}
}

func (g *Gateway) SendNotificationToClients(n *gnmi.Notification) {
	g.sendUpdateToClients(ctree.DetachedLeaf(n))
}

// StartGNMIServer will start the gNMI server that serves the Subscribe interface to downstream gNMI clients.
func (g *Gateway) StartGNMIServer(connMgr *connections.ConnectionManager) error {
	if g.config.ServerTLSCreds == nil {
		if g.config.ServerTLSCert == "" || g.config.ServerTLSKey == "" {
			return fmt.Errorf("no TLS creds; you must specify a TLS cert and key")
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
	// Initialize gNMI Proxy Subscribe server.
	gnmiServerOpts := &server.GNMIServerOpts{
		Config:  g.config,
		Cache:   connMgr.Cache(),
		Cluster: g.cluster,
		ConnMgr: connMgr,
	}
	subscribeSrv, err := server.NewServer(gnmiServerOpts)
	if err != nil {
		return fmt.Errorf("Could not instantiate gNMI server: %v", err)
	}
	gnmi.RegisterGNMIServer(srv, subscribeSrv)
	// Forward streaming updates to clients.
	g.AddClient(subscribeSrv.Update)
	// Register listening port and start serving.
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", g.config.ServerListenPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	go func() {
		err := srv.Serve(lis) // blocks
		g.config.Log.Error().Err(err).Msg("Error running gNMI server.")
	}()
	defer srv.Stop()
	ctx := context.Background()
	<-ctx.Done()
	return ctx.Err()
}

func (g *Gateway) ConnectToZookeeper() (*zk.Conn, error) {
	g.config.Log.Info().Msgf("Connecting to Zookeeper hosts: %v.", strings.Join(g.config.ZookeeperHosts, ", "))
	newConn, eventChan, err := zk.Connect(g.config.ZookeeperHosts, g.config.ZookeeperTimeout)
	if err != nil {
		g.config.Log.Error().Err(err).Msgf("Unable to connect to Zookeeper: %v", err)
		return nil, err
	}
	go g.zookeeperEventHandler(eventChan)
	g.config.Log.Info().Msg("Zookeeper connected.")
	return newConn, nil
}

func (g *Gateway) zookeeperEventHandler(zkEventChan <-chan zk.Event) {
	for {
		select {
		case event := <-zkEventChan:
			if event.State != zk.StateUnknown {
				switch event.State {
				case zk.StateConnected:
					g.config.Log.Info().Msg("Connected to Zookeeper.")
				case zk.StateConnecting:
					g.config.Log.Info().Msg("Attempting to connect to Zookeeper.")
				case zk.StateDisconnected:
					g.config.Log.Info().Msg("Zookeeper disconnected.")
				case zk.StateHasSession:
					g.config.Log.Info().Msg("Zookeeper session established.")
				default:
					g.config.Log.Info().Msgf("Got Zookeeper state update: %v", event.State.String())
				}
			}
		}
	}
}
