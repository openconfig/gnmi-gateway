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
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"time"
)

// Command line parameters
var (
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

	opts := &gateway.GatewayStartOpts{
		TargetLoader: targets.NewJSONFileTargetLoader(config),
	}

	if enablePrometheus {
		opts.Exporters = append(opts.Exporters, exporters.NewPrometheusExporter(config))
	}

	err := gateway.StartGateway(config, opts) // run forever (or until an error happens)
	if err != nil {
		config.Log.Error().Err(err).Msgf("Gateway exited with an error: %v", err)
		os.Exit(1)
	}
}

func parseArgs(config *configuration.GatewayConfig) {
	// Execution parameters
	flag.BoolVar(&config.EnableGNMIServer, "EnableServer", false, "Enable the gNMI server.")
	flag.BoolVar(&enablePrometheus, "EnablePrometheus", false, "Enable the Prometheus exporter.")
	flag.BoolVar(&printVersion, "version", false, "Print version and exit.")

	// Configuration Parameters
	flag.StringVar(&config.OpenConfigModelDirectory, "OpenConfigDirectory", "", "Directory (required to enable Prometheus exporter).")
	flag.StringVar(&config.TargetConfigurationJSONFile, "TargetConfigFile", "targets.json", "JSON file containing the target configurations (default: targets.json).")
	flag.DurationVar(&config.TargetConfigurationJSONFileReloadInterval, "TargetConfigInterval", 10*time.Second, "Interval to reload the JSON file containing the target configurations (default: 10s)")
	flag.DurationVar(&config.TargetDialTimeout, "TargetDialTimeout", 10*time.Second, "Dial timeout time (default: 10s)")

	flag.Parse()
}

func DefaultConfig() *configuration.GatewayConfig {
	config := configuration.NewDefaultGatewayConfig()
	return config
}
