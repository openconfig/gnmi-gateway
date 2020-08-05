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
	"fmt"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/exporters/prometheus"
	"github.com/openconfig/gnmi-gateway/gateway/loaders"
	"github.com/openconfig/gnmi-gateway/gateway/loaders/json"
	"os"
	"time"
)

// A simple example of how to configure the gateway.
func Example() {
	config := &configuration.GatewayConfig{
		EnableGNMIServer:    true,
		OpenConfigDirectory: "./oc-models",
		ServerListenPort:    2906,
		ServerTLSCert:       "server.crt",
		ServerTLSKey:        "server.key",
		TargetLoaders: &configuration.TargetLoadersConfig{
			Enabled:                []string{"json"},
			JSONFile:               "targets.json",
			JSONFileReloadInterval: 60 * time.Second,
		},
		TargetDialTimeout: 5 * time.Second,
		TargetLimit:       100,
		ZookeeperHosts:    []string{"127.0.0.1"},
		ZookeeperTimeout:  1 * time.Second,
	}

	gateway := NewGateway(config)
	err := gateway.StartGateway(&StartOpts{
		TargetLoaders: []loaders.TargetLoader{json.NewJSONFileTargetLoader(config)},
		Exporters: []exporters.Exporter{
			prometheus.NewPrometheusExporter(config),
		},
	}) // run forever (or until an error happens)
	if err != nil {
		fmt.Printf("Gateway exited with an error: %v", err)
		os.Exit(1)
	}
}
