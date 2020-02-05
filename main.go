// Copyright 2020 Netflix Inc
// Author: Colin McIntosh

package main

import (
	"flag"
	"fmt"
	"github.com/openconfig/gnmi/proto/gnmi"
	targetpb "github.com/openconfig/gnmi/proto/target"
	"github.com/rs/zerolog/log"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/connections"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/server"
	"time"
)

// Command line parameters
var (
	connect      bool
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

	if connect {
		config.Log.Info().Msg("Starting connection manager.")
		connMgr.Start()
	}

	if enableServer {
		config.Log.Info().Msg("Starting gNMI server.")
		go func() {
			if !connect {
				log.Warn().Msg("Starting the gNMI server without calling -connect will have no effect.")
			}

			if err := server.StartServer(config, connMgr.Cache()); err != nil {
				log.Error().Err(err).Msg("Unable to start gNMI server.")
				os.Exit(1)
			}
		}()
	}

	if connect || enableServer { // running modes
		config.Log.Info().Msg("Running.")
		select {} // block forever
	}
}

func parseArgs(config *gateway.GatewayConfig) {
	// Execution parameters
	flag.BoolVar(&connect, "connect", false, "Connect to targets.")
	flag.BoolVar(&enableServer, "enableServer", false, "Connect to targets.")
	flag.StringVar(&config.TargetConfigurations.Target["es04.sjc006"].Credentials.Username, "user", "", "Username for connecting to targets.")
	flag.StringVar(&config.TargetConfigurations.Target["es04.sjc006"].Credentials.Password, "password", "", "Password for connecting to targets.")
	flag.BoolVar(&printVersion, "version", false, "Print version and exit.")

	// Configuration Parameters
	flag.DurationVar(&config.TargetDialTimeout, "targetDialTimeout", 10*time.Second, "Dial timeout time (default: 10s)")

	flag.Parse()
}

func DefaultConfig() *gateway.GatewayConfig {
	config := gateway.NewDefaultGatewayConfig()
	config.TargetConfigurations = &targetpb.Configuration{
		Request: map[string]*gnmi.SubscribeRequest{
			"default": {
				Request: &gnmi.SubscribeRequest_Subscribe{
					Subscribe: &gnmi.SubscriptionList{
						Prefix: &gnmi.Path{},
						Subscription: []*gnmi.Subscription{
							{
								Path: &gnmi.Path{
									Elem: []*gnmi.PathElem{
										{Name: "interfaces"},
									},
								},
							},
						},
					},
				},
			},
		},
		Target: map[string]*targetpb.Target{
			"es04.sjc006": {
				Addresses: []string{"es04.sjc006.ix.nflxvideo.net:2906"},
				Credentials: &targetpb.Credentials{
					Username: "",
					Password: "",
				},
				Request: "default",
			},
		},
		Revision: 0,
	}
	return config
}
