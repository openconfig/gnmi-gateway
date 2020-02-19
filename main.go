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

package main

import (
	"flag"
	"fmt"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/connections"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/server"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"time"
)

// Command line parameters
var (
	enableServer     bool
	enablePrometheus bool
	printVersion     bool
)

var (
	version = "0.1.0"
)

func main() {
	if printVersion {
		fmt.Println(fmt.Sprintf("gnmi-gateway version %s", version))
		os.Exit(0)
	}

	config := DefaultConfig()
	parseArgs(config)

	connMgr, err := connections.NewConnectionManagerDefault(config)
	if err != nil {
		config.Log.Error().Err(err).Msg("Unable to create connection manager.")
		os.Exit(1)
	}
	config.Log.Info().Msg("Starting connection manager.")
	connMgr.Start()

	targetLoader := targets.NewJSONFileTargetLoader(config)
	go targetLoader.WatchConfiguration(connMgr.TargetConfigChan())

	if enableServer {
		config.Log.Info().Msg("Starting gNMI server.")
		go func() {
			if err := server.StartServer(config, connMgr.Cache()); err != nil {
				config.Log.Error().Err(err).Msg("Unable to start gNMI server.")
				os.Exit(1)
			}
		}()
	}

	if enablePrometheus {
		config.Log.Info().Msg("Starting Prometheus exporter.")
		prometheusExporter := exporters.NewPrometheusExporter(config, connMgr.Cache())
		err := prometheusExporter.Start()
		if err != nil {
			config.Log.Error().Err(err).Msg("Unable to start Prometheus exporter.")
			os.Exit(1)
		}
	}

	config.Log.Info().Msg("Running.")
	select {} // block forever
}

func parseArgs(config *gateway.GatewayConfig) {
	// Execution parameters
	flag.BoolVar(&enableServer, "EnableServer", false, "Enable the gNMI server.")
	flag.BoolVar(&enablePrometheus, "EnablePrometheus", false, "Enable the Prometheus exporter.")
	flag.BoolVar(&printVersion, "version", false, "Print version and exit.")

	// Configuration Parameters
	flag.StringVar(&config.OpenConfigModelDirectory, "OpenConfigDirectory", "", "Directory (required to enable Prometheus exporter).")
	flag.StringVar(&config.TargetConfigurationJSONFile, "TargetConfigFile", "targets.json", "JSON file containing the target configurations (default: targets.json).")
	flag.DurationVar(&config.TargetConfigurationJSONFileReloadInterval, "TargetConfigInterval", 10*time.Second, "Interval to reload the JSON file containing the target configurations (default: 10s)")
	flag.DurationVar(&config.TargetDialTimeout, "TargetDialTimeout", 10*time.Second, "Dial timeout time (default: 10s)")

	flag.Parse()
}

func DefaultConfig() *gateway.GatewayConfig {
	config := gateway.NewDefaultGatewayConfig()
	return config
}
