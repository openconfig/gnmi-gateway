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
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"strings"
	"syscall"
	"time"
)

// Main is the entry point for the command-line and it's a good example of how to call StartGateway but
// other than that you probably don't need Main for anything.
func Main() {
	config := configuration.NewDefaultGatewayConfig()
	ParseArgs(config)

	if PrintVersion {
		fmt.Println(fmt.Sprintf("gnmi-gateway version %s (Built %s)", Version, Buildtime))
		os.Exit(0)
	}

	var deferred []func()
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		config.Log.Info().Msg("Ctrl^C pressed.")
		for _, deferredFunc := range deferred {
			deferredFunc()
		}
		config.Log.Info().Msg("Exit.")
		os.Exit(0)
	}()

	debugCleanup, err := SetupDebugging(config)
	if err != nil {
		config.Log.Error().Err(err).Msgf("Unable to setup debugging: %v", err)
		os.Exit(1)
	} else {
		if debugCleanup != nil {
			deferred = append(deferred, debugCleanup)
		}
	}

	opts := &StartOpts{
		TargetLoader: targets.NewJSONFileTargetLoader(config),
	}

	if EnablePrometheus {
		opts.Exporters = append(opts.Exporters, exporters.NewPrometheusExporter(config))
	}

	gateway := NewGateway(config)
	err = gateway.StartGateway(opts) // run forever (or until an error happens)
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
	flag.StringVar(&CPUProfile, "CPUProfile", "", "Specify the name of the file for writing CPU profiling to enable the CPU profiling.")
	flag.BoolVar(&EnablePrometheus, "EnablePrometheus", false, "Enable the Prometheus exporter")
	flag.BoolVar(&LogCaller, "LogCaller", false, "Include the file and line number with each log message")
	flag.BoolVar(&PProf, "PProf", false, "Enable the pprof debugging web server.")
	flag.BoolVar(&PrintVersion, "version", false, "Print version and exit")

	// Configuration Parameters
	flag.BoolVar(&config.EnableServer, "EnableServer", false, "Enable the gNMI server")
	flag.StringVar(&config.OpenConfigDirectory, "OpenConfigDirectory", "", "Directory (required to enable Prometheus exporter)")
	flag.IntVar(&config.ServerPort, "ServerPort", 2906, "TCP port to run the gNMI server on (default: 2906)")
	flag.StringVar(&config.ServerTLSCert, "ServerTLSCert", "", "File containing the gNMI server TLS certificate (required to enable the gNMI server)")
	flag.StringVar(&config.ServerTLSKey, "ServerTLSKey", "", "File containing the gNMI server TLS key (required to enable the gNMI server)")
	flag.StringVar(&config.TargetJSONFile, "TargetJSONFile", "targets.json", "JSON file containing the target configurations (default: targets.json)")
	flag.DurationVar(&config.TargetJSONFileReloadInterval, "TargetJSONFileReloadInterval", 60*time.Second, "Interval to reload the JSON file containing the target configurations (default: 10s)")
	flag.DurationVar(&config.TargetDialTimeout, "TargetDialTimeout", 10*time.Second, "Dial timeout time (default: 10s)")
	flag.IntVar(&config.TargetLimit, "TargetLimit", 100, "Maximum number of targets that this instance will connect to at once (default: 100)")
	zkHosts := flag.String("ZookeeperHosts", "127.0.0.1:2181", "Comma separated (no spaces) list of zookeeper hosts including port (default: 127.0.0.1:2181)")
	flag.StringVar(&config.ZookeeperPrefix, "ZookeeperPrefix", "/gnmi/gateway/", "Prefix for the lock path in Zookeeper (default: /gnmi/gateway)")
	flag.DurationVar(&config.ZookeeperTimeout, "ZookeeperTimeout", 1*time.Second, "Zookeeper timeout time. Minimum is 1 second. Failover time is (ZookeeperTimeout * 2). (default: 1s)")

	flag.Parse()
	config.ZookeeperHosts = strings.Split(*zkHosts, ",")
}

// SetupDebugging optionally sets up debugging features including -LogCaller and -PProf.
func SetupDebugging(config *configuration.GatewayConfig) (func(), error) {
	var deferFuncs []func()

	if LogCaller {
		config.Log = config.Log.With().Caller().Logger()
	}

	if PProf {
		port := ":6161"
		go func() {
			if err := http.ListenAndServe(port, nil); err != nil {
				config.Log.Error().Err(err).Msgf("error starting pprof web server: %v", err)
			}
			config.Log.Info().Msgf("Launched pprof web server on %v", port)
		}()
	}

	if CPUProfile != "" {
		f, err := os.Create(CPUProfile)
		if err != nil {
			config.Log.Error().Err(err).Msgf("Unable to create CPU profiling file %s", CPUProfile)
			return nil, err
		}
		if err = pprof.StartCPUProfile(f); err != nil {
			config.Log.Error().Err(err).Msg("Unable to start CPU profiling")
			return nil, err
		}
		config.Log.Info().Msg("Started CPU profiling.")
		deferFuncs = append(deferFuncs, pprof.StopCPUProfile)
	}
	return func() {
		config.Log.Info().Msg("Cleaning up debugging.")
		for _, deferred := range deferFuncs {
			deferred()
		}
	}, nil
}
