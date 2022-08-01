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
	"fmt"

	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
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
	connMgr *connections.ConnectionManager
	config  *configuration.GatewayConfig
}

func (e *DebugExporter) Name() string {
	return Name
}

func (e *DebugExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)
	msg := utils.GNMINotificationPrettyString(notification)

	if len(notification.Update) > 0 {
		msg += " update_path_meta:[ "
		for _, u := range notification.Update {
			msg += fmt.Sprintf(
				"%v, ", e.config.GetPathMetadata(
					utils.GetTrimmedPath(notification.Prefix, u.Path)))
		}
		msg += "] "
	}

	e.config.Log.Info().Msg(msg)
}

func (e *DebugExporter) Start(connMgr *connections.ConnectionManager) error {
	_ = connMgr
	e.config.Log.Info().Msg("Starting Debug exporter.")
	return nil
}
