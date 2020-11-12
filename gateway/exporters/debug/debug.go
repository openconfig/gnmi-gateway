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

// Package debug provides an exporter that will log all received gNMI messages
// for debugging purposes.
package debug

import (
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
)

const Name = "debug"

var _ exporters.Exporter = new(DebugExporter)

func init() {
	exporters.Register(Name, NewDebugExporter)
}

func NewDebugExporter(config *configuration.GatewayConfig) exporters.Exporter {
	exporter := &DebugExporter{
		config: config,
	}
	return exporter
}

type DebugExporter struct {
	cache  *cache.Cache
	config *configuration.GatewayConfig
}

func (e *DebugExporter) Name() string {
	return Name
}

func (e *DebugExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)
	e.config.Log.Info().Msg(utils.GNMINotificationPrettyString(notification))
}

func (e *DebugExporter) Start(cache *cache.Cache) error {
	_ = cache
	e.config.Log.Info().Msg("Starting Debug exporter.")
	return nil
}
