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

// Package loaders provides an interface load target configurations into the
// connection manager.
package loaders

import (
	targetpb "github.com/openconfig/gnmi/proto/target"

	"github.com/mspiez/gnmi-gateway/gateway/configuration"
	"github.com/mspiez/gnmi-gateway/gateway/connections"
)

var Registry = make(map[string]func(config *configuration.GatewayConfig) TargetLoader)

// TargetLoader is an interface to load target configuration data.
// TargetLoader communicates with the ConnectionManager via
// TargetConnectionControl messages that contain targetpb.Configuration.
type TargetLoader interface {
	// Get the Configuration once.
	GetConfiguration() (*targetpb.Configuration, error)
	// Start the loader, if necessary.
	// Start will be called once by the gateway after StartGateway is called.
	Start() error
	// Start watching the configuration for changes and send the entire
	// configuration to the supplied channel when a change is detected.
	WatchConfiguration(chan<- *connections.TargetConnectionControl) error
}

func Register(name string, new func(config *configuration.GatewayConfig) TargetLoader) {
	Registry[name] = new
}

func New(name string, config *configuration.GatewayConfig) TargetLoader {
	exporter, exists := Registry[name]
	if !exists {
		return nil
	}
	return exporter(config)
}
