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

package stats

import (
	"fmt"
	"github.com/Netflix/spectator-go"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"time"
)

var Registry *spectator.Registry
var NoTags = make(map[string]string)

func init() {
	// Initialize an empty registry so we can start getting stats immediately.
	// The Registry needs to be recreated if we want to add a config.
	// TODO (cmcintosh): open a PR with Spectator to be able to set the config
	// 					 on an existing registry.
	Registry = spectator.NewRegistry(new(spectator.Config))
}

type Spectator struct {
	config   *configuration.GatewayConfig
	registry *spectator.Registry
}

func StartSpectator(config *configuration.GatewayConfig) (*Spectator, error) {
	if config.StatsSpectatorConfig == nil {
		if config.StatsSpectatorURI == "" {
			return nil, fmt.Errorf("StatsSpectatorConfig or StatsSpectatorURI must be set to start Spectator")
		}
		config.StatsSpectatorConfig = DefaultSpectatorConfig(config.StatsSpectatorURI)
	}

	s := &Spectator{
		config:   config,
		registry: spectator.NewRegistry(config.StatsSpectatorConfig),
	}
	oldRegistry := Registry
	Registry = s.registry

	for _, meter := range oldRegistry.Meters() {
		switch m := meter.(type) {
		case *spectator.Counter:
			Registry.CounterWithId(m.MeterId()).AddFloat(m.Measure()[0].Value())
		case *spectator.Gauge:
			Registry.GaugeWithId(m.MeterId()).Set(m.Measure()[0].Value())
		}
	}

	err := Registry.Start()
	if err != nil {
		return nil, fmt.Errorf("unable to start Spectator registry: %v", err)
	}

	return s, nil
}

func DefaultSpectatorConfig(uri string) *spectator.Config {
	return &spectator.Config{
		Frequency:  5 * time.Second,
		Timeout:    1 * time.Second,
		Uri:        uri,
		BatchSize:  10000,
		CommonTags: map[string]string{},
	}
}
