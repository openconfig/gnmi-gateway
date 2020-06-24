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
	"github.com/openconfig/gnmi/ctree"
	"github.com/openconfig/gnmi/proto/gnmi"
	_ "net/http/pprof"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/connections"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
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
	config     *configuration.GatewayConfig
}

// StartOpts is passed to StartGateway() and is used to set the running configuration
type StartOpts struct {
	// Loader for targets
	TargetLoader targets.TargetLoader
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
	connMgr, err := connections.NewConnectionManagerDefault(g.config)
	if err != nil {
		g.config.Log.Error().Err(err).Msg("Unable to create connection manager.")
		os.Exit(1)
	}
	g.config.Log.Info().Msg("Starting connection manager.")
	if err := connMgr.Start(); err != nil {
		g.config.Log.Error().Err(err).Msgf("Unable to start connection manager: %v", err)
		return err
	}

	// channel to listen for errors from child goroutines
	finished := make(chan error, 1)

	if g.config.TargetJSONFile != "" {
		opts.TargetLoader = targets.NewJSONFileTargetLoader(g.config)
	}

	go func() {
		err := opts.TargetLoader.Start()
		if err != nil {
			g.config.Log.Error().Err(err).Msgf("Unable to start target loader %T", opts.TargetLoader)
			finished <- err
		}
		opts.TargetLoader.WatchConfiguration(connMgr.TargetConfigChan())
	}()

	if g.config.EnableServer {
		g.config.Log.Info().Msg("Starting gNMI server.")
		go func() {
			if err := g.StartServer(connMgr.Cache()); err != nil {
				g.config.Log.Error().Err(err).Msg("Unable to start gNMI server.")
				finished <- err
			}
		}()
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

	connMgr.Cache().SetClient(g.sendUpdateToClients)

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
