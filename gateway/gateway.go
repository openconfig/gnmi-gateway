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
	"flag"
	"fmt"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/connections"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"strings"
	"time"
)

var (
	// Buildtime is set to the current time during the build process by GOLDFLAGS
	Buildtime string
	// Version is set to the current git tag during the build process by GOLDFLAGS
	Version string
)

var (
	enablePrometheus bool
	logCaller        bool
	printVersion     bool
)

// GatewayStartOpts is passed to StartGateway() and is used to set the running configuration
type GatewayStartOpts struct {
	// Loader for targets
	TargetLoader targets.TargetLoader
	// Exporters to run
	Exporters []exporters.Exporter
}

// Main is the entry point for the command-line and it's a good example of how to call StartGateway but
// other than that you probably don't need Main for anything.
func Main() {
	config := configuration.NewDefaultGatewayConfig()
	ParseArgs(config)

	if printVersion {
		fmt.Println(fmt.Sprintf("gnmi-gateway version %s (Built %s)", Version, Buildtime))
		os.Exit(0)
	}

	if true {
		config.Log = config.Log.With().Caller().Logger()
	}

	opts := &GatewayStartOpts{
		TargetLoader: targets.NewJSONFileTargetLoader(config),
	}

	if enablePrometheus {
		opts.Exporters = append(opts.Exporters, exporters.NewPrometheusExporter(config))
	}

	err := StartGateway(config, opts) // run forever (or until an error happens)
	if err != nil {
		config.Log.Error().Err(err).Msgf("Gateway exited with an error: %v", err)
		os.Exit(1)
	}
}

// ParseArgs will parse all of the command-line parameters and configured the associated attributes on the
// GatewayConfig. ParseArgs calls flag.Parse before returning so if you need to add arguments you should make
// any calls to flag before calling ParseArgs.
func ParseArgs(config *configuration.GatewayConfig) {
	// Execution parameters
	flag.BoolVar(&config.EnableServer, "EnableServer", false, "Enable the gNMI server")
	flag.BoolVar(&enablePrometheus, "EnablePrometheus", false, "Enable the Prometheus exporter")
	flag.BoolVar(&logCaller, "LogCaller", false, "Include the file and line number with each log message")
	flag.BoolVar(&printVersion, "version", false, "Print version and exit")

	// Configuration Parameters
	flag.StringVar(&config.OpenConfigDirectory, "OpenConfigDirectory", "", "Directory (required to enable Prometheus exporter)")
	flag.StringVar(&config.TargetJSONFile, "TargetJSONFile", "targets.json", "JSON file containing the target configurations (default: targets.json)")
	flag.DurationVar(&config.TargetJSONFileReloadInterval, "TargetJSONFileReloadInterval", 10*time.Second, "Interval to reload the JSON file containing the target configurations (default: 10s)")
	flag.DurationVar(&config.TargetDialTimeout, "TargetDialTimeout", 10*time.Second, "Dial timeout time (default: 10s)")
	flag.IntVar(&config.TargetLimit, "TargetLimit", 100, "Maximum number of targets that this instance will connect to at once (default: 100)")
	zkHosts := flag.String("ZookeeperHosts", "127.0.0.1:2181", "Comma separated (no spaces) list of zookeeper hosts including port (default: 127.0.0.1:2181)")
	flag.DurationVar(&config.ZookeeperTimeout, "ZookeeperTimeout", 1*time.Second, "Zookeeper timeout time. Minimum is 1 second. Failover time is (ZookeeperTimeout * 2). (default: 1s)")

	flag.Parse()
	config.ZookeeperHosts = strings.Split(*zkHosts, ",")
}

// StartGateway starts up all of the loaders and exporters provided by GatewayStartOpts. This is the
// primary way the server should be started.
func StartGateway(config *configuration.GatewayConfig, opts *GatewayStartOpts) error {
	config.Log.Info().Msg("Starting GNMI Gateway.")
	connMgr, err := connections.NewConnectionManagerDefault(config)
	if err != nil {
		config.Log.Error().Err(err).Msg("Unable to create connection manager.")
		os.Exit(1)
	}
	config.Log.Info().Msg("Starting connection manager.")
	if err := connMgr.Start(); err != nil {
		config.Log.Error().Err(err).Msgf("Unable to start connection manager: %v", err)
		return err
	}

	// channel to listen for errors from child goroutines
	finished := make(chan error, 1)

	go func() {
		err := opts.TargetLoader.Start()
		if err != nil {
			config.Log.Error().Err(err).Msgf("Unable to start target loader %T", opts.TargetLoader)
			finished <- err
		}
		opts.TargetLoader.WatchConfiguration(connMgr.TargetConfigChan())
	}()

	if config.EnableServer {
		config.Log.Info().Msg("Starting gNMI server.")
		go func() {
			if err := StartServer(config, connMgr.Cache()); err != nil {
				config.Log.Error().Err(err).Msg("Unable to start gNMI server.")
				finished <- err
			}
		}()
	}

	for _, exporter := range opts.Exporters {
		go func(exporter exporters.Exporter) {
			err := exporter.Start(connMgr.Cache())
			if err != nil {
				config.Log.Error().Err(err).Msgf("Unable to start exporter %T", exporter)
				finished <- err
			}
			// TODO: Call SetClient here as it's pretty universal.
		}(exporter)
	}

	return <-finished
}
