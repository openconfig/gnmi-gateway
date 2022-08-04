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

// Package configuration contains the GatewayConfig type that is used by the gateway package
// and all of it's sub-packages.
package configuration

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Netflix/spectator-go"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog"
	"google.golang.org/grpc/credentials"
)

// GatewayConfig contains all of the configurables and tunables for various components of the gateway.
// Many of these options may be set via command-line flags. See main.go for details on flags that
// are available.
type GatewayConfig struct {
	mu sync.RWMutex
	// ClientTLSConfig are the gNMI client TLS credentials. Setting this will enable client TLS.
	// TODO (cmcintosh): Add options to set client certificates by path (i.e. like the server TLS creds).
	ClientTLSConfig *tls.Config `ignored:"true"`
	// EnableGNMIServer will run the gNMI server (the Subscribe server). TLS options are also required
	// for the gNMI server to be enabled.
	EnableGNMIServer bool `json:"enable_gnmi_server"`
	// Exporters contains the configuration for the included exporters.
	Exporters *ExportersConfig `json:"exporters"`
	// By default, the gnmi cache suppresses the notifications if there aren't
	// any changes. However, gnmi-gw can be configured to use sampled subscribe
	// notifications. If this flag is enabled, such notifications will be emitted.
	EmitStaleUpdates bool `json:"emit_stale_updates"`
	// GatewayTransitionBufferSize tunes the size of the buffer between targets and exporters/clients.
	GatewayTransitionBufferSize uint64 `json:"gateway_transition_buffer_size"`
	// Log is the logger used by the gateway code and gateway packages.
	Log zerolog.Logger
	// LogCaller will add the file path and line number to all log messages.
	LogCaller bool `json:"log_caller"`
	// OpenConfigDirectory is the folder path to a clone of github.com/openconfig/public.
	// OpenConfigDirectory is required for value typing if any exporters are enabled.
	OpenConfigDirectory string `json:"openconfig_directory"`
	// ServerAddress is the address where other cluster members can reach the gNMI server.
	// The first assigned IP address is used if the parameter is not provided.
	ServerAddress string `json:"server_address"`
	// ServerPort is the TCP port where other cluster members can reach the gNMI server.
	// ServerListenPort is used if the parameter is not provided.
	ServerPort int `json:"server_port"`
	// ServerListenAddress is the interface IP address the gNMI server will listen on.
	ServerListenAddress string `json:"server_listen_address"`
	// ServerListenPort is the TCP port the gNMI server will listen on.
	ServerListenPort int `json:"server_listen_port"`
	// ServerTLSCreds are the gNMI Server TLS credentials. You must specify either this or both
	// ServerTLSCert and ServerTLSKey if you set -EnableGNMIServer.
	ServerTLSCreds credentials.TransportCredentials
	// ServerTLSCert is the path to the file containing the PEM-encoded x509 gNMI server TLS certificate.
	// See the gateway package for instructions for generating a self-signed certificate.
	ServerTLSCert string `json:"server_tls_cert"`
	// ServerTLSCert is the path to the file containing the PEM-encoded x509 gNMI server TLS key.
	// See the gateway package for instructions for generating a self-signed certificate key.
	ServerTLSKey string `json:"server_tls_key"`
	// StatsSpectatorConfig is the configuration used for Spectator.
	// Either this or StatsSpectatorURI must be set to enable sending internal
	// gnmi-gateway metrics to Atlas.
	StatsSpectatorConfig *spectator.Config
	// StatsSpectatorURI is the URI used for sending metrics to Spectator.
	// Either this or StatsSpectatorConfig must be set to enable sending internal
	// gnmi-gateway metrics to Atlas.
	StatsSpectatorURI string `json:"stats_spectator_uri"`
	// TargetLoaders contains the configuration for the included target loaders.
	TargetLoaders *TargetLoadersConfig `json:"target_loaders"`
	// TargetDialTimeout is the network transport timeout time for dialing the target connection.
	TargetDialTimeout time.Duration `json:"target_dial_timeout"`
	// TargetLimit is the maximum number of targets that this instance will connect to at once.
	// TargetLimit can also be considered the number of "connection slots" available on this
	// gateway instance. For failover of targets to other cluster members to complete fully
	// there needs to be sufficient connection slots available on other cluster members.
	TargetLimit int `json:"target_limit"`
	// UpdateRejections are a list of gNMI paths that may be matched against for messages that
	// are to be dropped prior to being inserted into the cache. This is useful for blocking
	// portions of the tree that you are not interested in but still need a subscription for.
	UpdateRejections [][]*gnmipb.PathElem
	// ZookeeperHosts are all of the addresses for members of your Zookeeper ensemble.
	// Setting ZookeeperHosts will enable clustering. -EnableGNMIServer is required
	// for clustering to be enabled.
	ZookeeperHosts []string `json:"zookeeper_hosts"`
	// ZookeeperPrefix is the prefix for the lock path in Zookeeper (e.g. /gnmi/gateway).
	ZookeeperPrefix string `json:"zookeeper_prefix"`
	// ZookeeperTimeout is the Zookeeper connection transport timeout time.
	// Minimum ZookeeperTimeout is ('your Zookeeper server tickTime' * 2).
	// Failover of targets will take (ZookeeperTimeout * 2) time.
	ZookeeperTimeout time.Duration `json:"zookeeper_timeout"`
	// EnableClustering will dictate if you want clustering turned on
	// or just having zookeeper hosts for target loading
	EnableClustering bool `json:"enable_clustering"`
	// ExporterMetadataAllowlist will tell the exporter which metadata to include
	// in the gnmi notification before exporting the metric
	ExporterMetadataAllowlist []string `json:"exporter_metadata_allowlist"`
	// per-path metadata
	// TODO: consider using a tree
	pathMeta map[string]*map[string]string
}

type ExportersConfig struct {
	// Enabled contains the list of named exporters that should be started.
	Enabled []string `json:"enabled"`

	// KafkaBatchBytes is the max message bytes that will be buffered before
	// flushing messages to a partition.
	KafkaBatchBytes int64 `json:"kafka_batch_bytes"`
	// KafkaBatchSize is the max number of messages that will be buffered before
	// flushing messages to a partition.
	KafkaBatchSize int `json:"kafka_batch_size"`
	// KafkaBatchTimeout is the max amount of time that will pass before
	// buffered messages are flushed to a partition.
	KafkaBatchTimeout time.Duration `json:"kafka_batch_timeout"`
	// KafkaBrokers contains the list of Kafka broker addresses and ports to
	// connect to.
	KafkaBrokers []string `json:"kafka_brokers"`
	// KafkaLogging enables info level logging from the Kafka writer. Kafka
	// error logging is always enabled.
	KafkaLogging bool `json:"kafka_logging"`
	// KafkaTopic is the name of the Kafka topic to send exported gNMI messages
	// to.
	KafkaTopic string `json:"kafka_topic"`

	// InfluxDBTarget is the target URL for influx connections
	InfluxDBTarget string `json:"influxdb_target"`
	// InfluxDBOrg is organizaion workspace
	InfluxDBOrg string `json:"influxdb_org"`
	// InfluxDBBucket is the named collection for storing time series data
	InfluxDBBucket string `json:"influxdb_bucket"`
	// InfluxDBToken is used to authenticate the session
	InfluxDBToken string `json:"infludb_token"`
	// InfluxDBBatchSize the number of lines or individual data points in a
	// line protocol batch.
	InfluxDBBatchSize uint `json:"influxdb_batch_size"`

	// StatsdHost is the target address for the statsd exporter
	StatsdHost string `json:"statsd_host"`
}

type TargetLoadersConfig struct {
	// Enabled contains the list of named target loaders that should be started.
	Enabled []string `json:"enabled"`

	// JSONFile is the path to a JSON file containing the configuration for
	// gNMI targets and subscribe requests. The file will be checked for
	// changes every TargetJSONFileReloadInterval.
	JSONFile string `json:"json_file"`
	// JSONFileReloadInterval is the interval to check TargetJSONFile for changes.
	JSONFileReloadInterval time.Duration `json:"json_file_reload_interval"`

	// NetBoxAPIKey is a valid API for the NetBox instance.
	NetBoxAPIKey string
	// NetBoxDeviceGNMIPort is the port on the device that the gNMI server is
	// running on.
	NetBoxDeviceGNMIPort int
	// NetBoxDeviceUsername is the username of the devices to connect to.
	// TODO (cmcintosh): replace this with NetBox secrets
	NetBoxDeviceUsername string
	// NetBoxDevicePossword is the passowrd of the devices to connect to.
	// TODO (cmcintosh): replace this with NetBox secrets
	NetBoxDevicePassword string
	// NetBoxHost is the address and port of the NetBox server.
	NetBoxHost string
	// NetBoxIncludeTag is a tag to filter devices in NetBox by. If no tag is
	// supplied all devices in NetBox with valid details are included. You
	// probably want to set this unless all of your devices support gNMI.
	NetBoxIncludeTag string
	// NetBoxReloadInterval is the frequency at which to check NetBox for
	// changes to devices.
	NetBoxReloadInterval time.Duration
	// NetBoxSubscribePaths is a list of XPath subscription paths for the
	// devices in NetBox.
	NetBoxSubscribePaths []string

	// SimpleFile is the path to a YAML file containing a simple target config.
	SimpleFile string `json:"simple_file"`
	// SimpleFileReloadInterval is the interval to check SimpleFile for changes.
	SimpleFileReloadInterval time.Duration `json:"simple_file_reload_interval"`

	ZookeeperReloadInterval time.Duration `json:"zookeeper_reload_interval"`
}

func NewDefaultGatewayConfig() *GatewayConfig {
	config := &GatewayConfig{
		Exporters:     new(ExportersConfig),
		Log:           zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.InfoLevel),
		TargetLoaders: new(TargetLoadersConfig),
		pathMeta: make(map[string]*map[string]string, 0),
	}
	return config
}

func NewGatewayConfigFromFile(filePath string) (*GatewayConfig, error) {
	var config GatewayConfig
	err := PopulateGatewayConfigFromFile(&config, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create new config from file: %v", err)
	}
	return &config, nil
}

func PopulateGatewayConfigFromFile(config *GatewayConfig, filePath string) error {
	path := filepath.Clean(filePath)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file at '%s': %v", path, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(config); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	if config.TargetDialTimeout < time.Second {
		config.TargetDialTimeout *= time.Second
	}
	if config.TargetLoaders.JSONFileReloadInterval < time.Second {
		config.TargetLoaders.JSONFileReloadInterval *= time.Second
	}
	if config.ZookeeperTimeout < time.Second {
		config.ZookeeperTimeout *= time.Second
	}
	return nil
}

// Metadata returns the per-path metadata structures.
// Children inherit parent metadata.
func (c *GatewayConfig) GetPathMetadata(path string) map[string]string {
	md := map[string]string{}
	defer c.mu.RUnlock()
	c.mu.RLock()

	path = strings.TrimRight(path, "/")

	for {
		foundMeta, found := c.pathMeta[path]
		if found {
			for key, val := range *foundMeta {
				// Parent metadata doesn't override children metadata.
				if _, found := md[key]; !found {
					md[key] = val
				}
			}
		}

		if path == "" || path == "." || path == "/" {
			break
		}
		parent := filepath.Dir(path)
		path = strings.TrimRight(parent, "/")
	}

	return md
}

func (c *GatewayConfig) AddPathMetadata(path string, meta map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	path = strings.TrimRight(path, "/")
	pathMeta, found := c.pathMeta[path]
	if !found {
		pathMeta = &map[string]string{}
		c.pathMeta[path] = pathMeta
	}

	for key, val := range meta {
		(*pathMeta)[key] = val
	}
}

func (c *GatewayConfig) ReplacePathMetadata(
	meta map[string]*map[string]string,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pathMeta = meta
}
