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
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/targets"
	"time"
)

// A simple example of how to configure the gateway.
func Example() {
	config := &configuration.GatewayConfig{
		EnableServer:                 true,
		OpenConfigDirectory:          "./oc-models",
		ServerPort:                   2906,
		ServerTLSCert:                "server.crt",
		ServerTLSKey:                 "server.key",
		TargetJSONFile:               "targets.json",
		TargetJSONFileReloadInterval: 60 * time.Second,
		TargetDialTimeout:            5 * time.Second,
		TargetLimit:                  100,
		ZookeeperHosts:               []string{"127.0.0.1"},
		ZookeeperTimeout:             1 * time.Second,
	}

	gateway := NewGateway(config)
	err := gateway.StartGateway(&StartOpts{
		TargetLoader: targets.NewJSONFileTargetLoader(config),
		Exporters: []exporters.Exporter{
			exporters.NewPrometheusExporter(config),
		},
	}) // run forever (or until an error happens)
	if err != nil {
		fmt.Printf("Gateway exited with an error: %v", err)
		os.Exit(1)
	}
}
