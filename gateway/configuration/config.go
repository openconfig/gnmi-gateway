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

package configuration

import (
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/credentials"
	"time"
)

type GatewayConfig struct {
	// Logger used by the Gateway code
	Log zerolog.Logger

	// OpenConfig Models 'public' folder location
	OpenConfigModelDirectory string

	// Enable GNMI Server
	EnableGNMIServer bool
	// Port to server the gNMI server on
	ServerPort int
	// gNMI Server TLS credentials. You must specify either this or both ServerTLSCert & ServerTLSKey if you -EnableServer
	ServerTLSCreds credentials.TransportCredentials
	// gNMI Server TLS Cert
	ServerTLSCert string
	// gNMI Server TLS Key
	ServerTLSKey string

	// Configs for connections to targets
	TargetConfigurationJSONFile string
	// Interval to reload the target configuration JSON file.
	TargetConfigurationJSONFileReloadInterval time.Duration
	// Timeout for dialing the target connection
	TargetDialTimeout time.Duration
	// Maximum number of targets that this instance will connect to at once.
	TargetLimit int
	// Updates to be dropped prior to being inserted into the cache
	UpdateRejections [][]*gnmipb.PathElem

	// All of the hosts in your Zookeeper cluster (or single Zookeeper instance)
	ZookeeperHosts []string
	// Zookeeper timeout time. Minimum is 1 second. Failover time is (ZookeeperTimeout * 2).
	ZookeeperTimeout time.Duration
}

func NewDefaultGatewayConfig() *GatewayConfig {
	config := &GatewayConfig{
		Log: log.Logger,
	}
	return config
}
