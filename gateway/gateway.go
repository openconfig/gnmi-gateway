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

package gateway

import (
	"flag"
	"fmt"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/connections"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"time"
)

var (
	Version = "0.1.0"
)

var (
	EnablePrometheus bool
	PrintVersion     bool
)

type GatewayStartOpts struct {
	// Loader for targets
	TargetLoader targets.TargetLoader
	// Exporters to run
	Exporters []exporters.Exporter
}

func Main() {
	if PrintVersion {
		fmt.Println(fmt.Sprintf("gnmi-gateway version %s", Version))
		os.Exit(0)
	}

	config := configuration.NewDefaultGatewayConfig()
	ParseArgs(config)

	opts := &GatewayStartOpts{
		TargetLoader: targets.NewJSONFileTargetLoader(config),
	}

	if EnablePrometheus {
		opts.Exporters = append(opts.Exporters, exporters.NewPrometheusExporter(config))
	}

	err := StartGateway(config, opts) // run forever (or until an error happens)
	if err != nil {
		config.Log.Error().Err(err).Msgf("Gateway exited with an error: %v", err)
		os.Exit(1)
	}
}

func ParseArgs(config *configuration.GatewayConfig) {
	// Execution parameters
	flag.BoolVar(&config.EnableGNMIServer, "EnableServer", false, "Enable the gNMI server.")
	flag.BoolVar(&EnablePrometheus, "EnablePrometheus", false, "Enable the Prometheus exporter.")
	flag.BoolVar(&PrintVersion, "version", false, "Print version and exit.")

	// Configuration Parameters
	flag.StringVar(&config.OpenConfigModelDirectory, "OpenConfigDirectory", "", "Directory (required to enable Prometheus exporter).")
	flag.StringVar(&config.TargetConfigurationJSONFile, "TargetConfigFile", "targets.json", "JSON file containing the target configurations (default: targets.json).")
	flag.DurationVar(&config.TargetConfigurationJSONFileReloadInterval, "TargetConfigInterval", 10*time.Second, "Interval to reload the JSON file containing the target configurations (default: 10s)")
	flag.DurationVar(&config.TargetDialTimeout, "TargetDialTimeout", 10*time.Second, "Dial timeout time (default: 10s)")

	flag.Parse()
}

func StartGateway(config *configuration.GatewayConfig, opts *GatewayStartOpts) error {
	config.Log.Info().Msg("Starting GNMI Gateway.")
	connMgr, err := connections.NewConnectionManagerDefault(config)
	if err != nil {
		config.Log.Error().Err(err).Msg("Unable to create connection manager.")
		os.Exit(1)
	}
	config.Log.Info().Msg("Starting connection manager.")
	connMgr.Start()

	go opts.TargetLoader.WatchConfiguration(connMgr.TargetConfigChan())

	// channel to listen for errors from child goroutines
	finished := make(chan error, 1)

	if config.EnableGNMIServer {
		config.Log.Info().Msg("Starting gNMI server.")
		go func(finished chan error) {
			if err := StartServer(config, connMgr.Cache()); err != nil {
				config.Log.Error().Err(err).Msg("Unable to start gNMI server.")
				finished <- err
			}
		}(finished)
	}

	for _, exporter := range opts.Exporters {
		go func(finished chan error, exporter exporters.Exporter) {
			err := exporter.Start(connMgr.Cache())
			if err != nil {
				config.Log.Error().Err(err).Msg("Unable to start Prometheus exporter.")
				finished <- err
			}
		}(finished, exporter)
	}

	return <-finished
}
