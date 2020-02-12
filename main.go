// Copyright 2020 Netflix Inc
// Author: Colin McIntosh (colin@netflix.com)

package main

import (
	"flag"
	"fmt"
	"github.com/rs/zerolog/log"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/connections"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/server"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"time"
)

// Command line parameters
var (
	enableServer bool
	printVersion bool
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
		log.Error().Err(err).Msg("Unable to create connection manager.")
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
				log.Error().Err(err).Msg("Unable to start gNMI server.")
				os.Exit(1)
			}
		}()
	}

	config.Log.Info().Msg("Running.")
	select {} // block forever
}

func parseArgs(config *gateway.GatewayConfig) {
	// Execution parameters
	flag.BoolVar(&enableServer, "enableServer", false, "Connect to targets.")
	flag.BoolVar(&printVersion, "version", false, "Print version and exit.")

	// Configuration Parameters
	flag.StringVar(&config.TargetConfigurationJSONFile, "targetConfigFile", "targets.json", "JSON file containing the target configurations (default: targets.json).")
	flag.DurationVar(&config.TargetConfigurationJSONFileReloadInterval, "targetConfigInterval", 10*time.Second, "Interval to reload the JSON file containing the target configurations (default: 10s)")
	flag.DurationVar(&config.TargetDialTimeout, "targetDialTimeout", 10*time.Second, "Dial timeout time (default: 10s)")

	flag.Parse()
}

func DefaultConfig() *gateway.GatewayConfig {
	config := gateway.NewDefaultGatewayConfig()
	return config
}
