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
	"strings"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/all"
	_ "github.com/openconfig/gnmi-gateway/gateway/loaders/all"
)

// Main is the entry point for the command-line and it's a good example of how to call StartGateway but
// other than that you probably don't need Main for anything.
func Main() {
	config := configuration.NewDefaultGatewayConfig()
	err := ParseArgs(config)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

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

	opts := new(StartOpts)

	gateway := NewGateway(config)
	err = gateway.StartGateway(opts) // run forever (or until an error happens)
	if err != nil {
		config.Log.Error().Msgf("Gateway exited with an error: %v", err)
		os.Exit(1)
	}
}

// ParseArgs will parse all of the command-line parameters and configured the associated attributes on the
// GatewayConfig. ParseArgs calls flag.Parse before returning so if you need to add arguments you should make
// any calls to flag before calling ParseArgs.
func ParseArgs(config *configuration.GatewayConfig) error {
	// Execution parameters
	flag.StringVar(&CPUProfile, "CPUProfile", "", "Specify the name of the file for writing CPU profiling to enable the CPU profiling")
	flag.BoolVar(&PProf, "PProf", false, "Enable the pprof debugging web server")
	flag.BoolVar(&PrintVersion, "version", false, "Print version and exit")

	// Configuration Parameters
	configFile := flag.String("ConfigFile", "", "Path of the gateway configuration JSON file.")
	flag.BoolVar(&config.EnableGNMIServer, "EnableGNMIServer", false, "Enable the gNMI server")
	exporters := flag.String("Exporters", "", "Comma-separated list of Exporters to enable.")
	flag.Int64Var(&config.Exporters.KafkaBatchBytes, "ExporterKafkaBatchBytes", 1048576, "Max bytes that will be buffered before flushing messages to a Kafka partition")
	flag.IntVar(&config.Exporters.KafkaBatchSize, "ExporterKafkaBatchSize", 10000, "Max number of messages that will be buffered before flushing messages to a Kafka partition")
	flag.DurationVar(&config.Exporters.KafkaBatchTimeout, "ExporterKafkaBatchTimeout", 1*time.Second, "Max seconds between flushing messages to a Kafka partition")
	exporterKafkaBrokers := flag.String("ExporterKafkaBrokers", "", "Comma-separated list of Kafka broker addresses and ports for the Kafka Exporter to connect to")
	flag.BoolVar(&config.Exporters.KafkaLogging, "ExporterKafkaLogging", false, "Enables info level logging from the Kafka writer. Error level logging is always enabled")
	flag.StringVar(&config.Exporters.KafkaTopic, "ExporterKafkaTopic", "", "Kafka topic to send exported gNMI messages to.")

	flag.StringVar(&config.Exporters.InfluxDBTarget, "ExportersInfluxDBTarget", "http://localhost:8086", "InfluxDB target URL (default is http://localhost:8086")
	flag.StringVar(&config.Exporters.InfluxDBToken, "ExportersInfluxDBToken", "", "Sets the InfluxDB authentication token")
	flag.StringVar(&config.Exporters.InfluxDBOrg, "ExportersInfluxDBOrg", "", "Sets the InfluxDB organization name")
	flag.StringVar(&config.Exporters.InfluxDBBucket, "ExportersInfluxDBBucket", "", "Sets the InfluxDB bucket name")
	flag.UintVar(&config.Exporters.InfluxDBBatchSize, "ExportersInfluxDBBatchSize", 20, "Sets the writer batch size for InfluxDB records (default is 20")

	flag.Uint64Var(&config.GatewayTransitionBufferSize, "GatewayTransitionBufferSize", 100000, "Tunes the size of the buffer between targets and exporters/clients")
	flag.BoolVar(&config.LogCaller, "LogCaller", false, "Include the file and line number with each log message")
	flag.StringVar(&config.OpenConfigDirectory, "OpenConfigDirectory", "", "Directory (required to enable Prometheus exporter)")
	flag.StringVar(&config.ServerAddress, "ServerAddress", "", "The IP address where other cluster members can reach the gNMI server. The first assigned IP address is used if the parameter is not provided")
	flag.IntVar(&config.ServerPort, "ServerPort", 0, "The TCP port where other cluster members can reach the gNMI server. ServerListenPort is used if the parameter is not provided")
	flag.StringVar(&config.ServerListenAddress, "ServerListenAddress", "0.0.0.0", "The interface IP address the gNMI server will listen on")
	flag.IntVar(&config.ServerListenPort, "ServerListenPort", 9339, "TCP port to run the gNMI server on")
	flag.StringVar(&config.ServerTLSCert, "ServerTLSCert", "", "File containing the gNMI server TLS certificate (required to enable the gNMI server)")
	flag.StringVar(&config.ServerTLSKey, "ServerTLSKey", "", "File containing the gNMI server TLS key (required to enable the gNMI server)")
	flag.StringVar(&config.TargetLoaders.SimpleFile, "SimpleFile", "", "Simple YAML file containing the target configurations")
	flag.DurationVar(&config.TargetLoaders.SimpleFileReloadInterval, "SimpleFileReloadInterval", 30*time.Second, "Interval to reload the simple YAML file containing the target configurations")
	flag.StringVar(&config.StatsSpectatorURI, "StatsSpectatorURI", "", "URI for Atlas server to send Spectator metrics to (required to enable sending internal gateway stats to Atlas)")
	targetLoaders := flag.String("TargetLoaders", "", "Comma-separated list of Target Loaders to enable.")
	flag.StringVar(&config.TargetLoaders.JSONFile, "TargetJSONFile", "", "JSON file containing the target configurations")
	flag.DurationVar(&config.TargetLoaders.JSONFileReloadInterval, "TargetJSONFileReloadInterval", 30*time.Second, "Interval to reload the JSON file containing the target configurations")
	flag.DurationVar(&config.TargetDialTimeout, "TargetDialTimeout", 10*time.Second, "Dial timeout time")
	flag.IntVar(&config.TargetLimit, "TargetLimit", 100, "Maximum number of targets that this instance will connect to at once")
	flag.StringVar(&config.TargetLoaders.NetBoxAPIKey, "TargetNetBoxAPIKey", "", "API Key for NetBox target loader")
	flag.IntVar(&config.TargetLoaders.NetBoxDeviceGNMIPort, "TargetNetBoxDeviceGNMIPort", 0, "The port that the gNMI is served from on devices loaded from NetBox ")
	flag.StringVar(&config.TargetLoaders.NetBoxDeviceUsername, "TargetNetBoxDeviceUsername", "", "The port that the gNMI is served from on devices loaded from NetBox ")
	flag.StringVar(&config.TargetLoaders.NetBoxDevicePassword, "TargetNetBoxDevicePassword", "", "The port that the gNMI is served from on devices loaded from NetBox ")
	flag.StringVar(&config.TargetLoaders.NetBoxHost, "TargetNetBoxHost", "", "The address and port where the NetBox API can be reached")
	flag.StringVar(&config.TargetLoaders.NetBoxIncludeTag, "TargetNetBoxIncludeTag", "", "A tag to filter devices loaded from NetBox")
	flag.DurationVar(&config.TargetLoaders.NetBoxReloadInterval, "TargetNetBoxReloadInterval", 3*time.Minute, "The frequency at which to check NetBox for new or changed devices.")
	flag.DurationVar(&config.TargetLoaders.ZookeeperReloadInterval, "ZookeeperReloadInterval", 3*time.Minute, "The frequency at which to check Zookeeper for new or changed devices.")
	netboxSubscribePaths := flag.String("TargetNetBoxSubscribePaths", "", "Comma separated (no spaces) list of paths to subscribe to for devices loaded from NetBox")
	zkHosts := flag.String("ZookeeperHosts", "", "Comma separated (no spaces) list of zookeeper hosts including port")
	flag.StringVar(&config.ZookeeperPrefix, "ZookeeperPrefix", "/gnmi/gateway/", "Prefix for the lock path in Zookeeper")
	flag.DurationVar(&config.ZookeeperTimeout, "ZookeeperTimeout", 1*time.Second, "Zookeeper timeout time. Minimum is 1 second. Failover time is (ZookeeperTimeout * 2)")
	flag.BoolVar(&config.EnableClustering, "EnableClustering", true, "Flag to check if clustering should be enabled or not")
	flag.Parse()
	config.Exporters.Enabled = cleanSplit(*exporters)
	config.Exporters.KafkaBrokers = cleanSplit(*exporterKafkaBrokers)
	config.TargetLoaders.Enabled = cleanSplit(*targetLoaders)
	config.TargetLoaders.NetBoxSubscribePaths = cleanSplit(*netboxSubscribePaths)
	config.ZookeeperHosts = cleanSplit(*zkHosts)

	if *configFile != "" {
		err := configuration.PopulateGatewayConfigFromFile(config, *configFile)
		if err != nil {
			return fmt.Errorf("failed to populate config from file: %v", err)
		}
	}

	err := envconfig.Process("gateway", config)
	if err != nil {
		return fmt.Errorf("failed to read environment variable configuration: %v", err)
	}
	return nil
}

func cleanSplit(in string) []string {
	var out []string
	for _, s := range strings.Split(in, ",") {
		sTrimmed := strings.TrimSpace(s)
		if sTrimmed != "" {
			out = append(out, sTrimmed)
		}
	}
	return out
}

// SetupDebugging optionally sets up debugging features including -LogCaller and -PProf.
func SetupDebugging(config *configuration.GatewayConfig) (func(), error) {
	var deferFuncs []func()

	if config.LogCaller {
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
