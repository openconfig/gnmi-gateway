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

// Package exporters provides an interface to export gNMI notifications
// to other systems or data formats.
package exporters

//go:generate mockgen -destination=exporter_mock_test.go -package=exporters_test github.com/mspiez/gnmi-gateway/gateway/exporters Exporter

import (
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"

	"github.com/mspiez/gnmi-gateway/gateway/configuration"
)

var Registry = make(map[string]func(config *configuration.GatewayConfig) Exporter)

// Exporter is an interface to send data to other systems and protocols.
type Exporter interface {
	// Name must return unique exporter name that will be used for registration
	// and recording internal stats.
	Name() string
	// Start will be called once by the gateway.Gateway after StartGateway
	// is called. It will receive a pointer to the cache.Cache that
	// receives all of the updates from gNMI targets that the gateway has a
	// subscription for. If Start returns an error the gateway will fail to
	// start with an error.
	Start(*cache.Cache) error
	// Export will be called once for every gNMI notification that is inserted
	// into the cache.Cache. Export should complete as quickly as possible to
	// prevent delays in the system and upstream gNMI clients.
	// Export receives the leaf parameter which is a *ctree.Leaf type and
	// has a value of type *gnmipb.Notification. You can access the notification
	// with a type assertion: leaf.Value().(*gnmipb.Notification)
	Export(leaf *ctree.Leaf)
}

func Register(name string, new func(config *configuration.GatewayConfig) Exporter) {
	Registry[name] = new
}

func New(name string, config *configuration.GatewayConfig) Exporter {
	exporter, exists := Registry[name]
	if !exists {
		return nil
	}
	return exporter(config)
}
