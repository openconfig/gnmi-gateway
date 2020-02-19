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
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
}

func NewDefaultGatewayConfig() *GatewayConfig {
	config := &GatewayConfig{
		Log: log.Logger,
	}
	return config
}
