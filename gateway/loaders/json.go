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

package loaders

import (
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	targetpb "github.com/openconfig/gnmi/proto/target"
	"github.com/openconfig/gnmi/target"
	"os"
	"time"
)

type JSONFileTargetLoader struct {
	config   *configuration.GatewayConfig
	file     string
	interval time.Duration
	last     *targetpb.Configuration
}

func NewJSONFileTargetLoader(config *configuration.GatewayConfig) TargetLoader {
	return &JSONFileTargetLoader{
		config:   config,
		file:     config.TargetJSONFile,
		interval: config.TargetJSONFileReloadInterval,
	}
}

func (m *JSONFileTargetLoader) GetConfiguration() (*targetpb.Configuration, error) {
	f, err := os.Open(m.file)
	if err != nil {
		return nil, fmt.Errorf("could not open configuration file %q: %v", m.file, err)
	}
	defer func() {
		if err = f.Close(); err != nil {
			m.config.Log.Error().Err(err).Msg("Error closing configuration file.")
		}
	}()
	configs := new(targetpb.Configuration)
	if err := jsonpb.Unmarshal(f, configs); err != nil {
		return nil, fmt.Errorf("could not parse configuration from %q: %v", m.file, err)
	}
	if err := target.Validate(configs); err != nil {
		return nil, fmt.Errorf("configuration in %q is invalid: %v", m.file, err)
	}
	return configs, nil
}

func (m *JSONFileTargetLoader) Start() error {
	return nil // nothing to start
}

func (m *JSONFileTargetLoader) WatchConfiguration(targetChan chan<- *connections.TargetConnectionControl) error {
	for {
		targetConfig, err := m.GetConfiguration()
		if err != nil {
			m.config.Log.Error().Err(err).Msgf("Unable to get target configuration.")
		} else {
			controlMsg := new(connections.TargetConnectionControl)
			if m.last != nil {
				for targetName := range m.last.Target {
					_, exists := targetConfig.Target[targetName]
					if !exists {
						controlMsg.Remove = append(controlMsg.Remove, targetName)
					}
				}
			}
			controlMsg.Insert = targetConfig
			m.last = targetConfig

			targetChan <- controlMsg
		}
		time.Sleep(m.interval)
	}
}

//func WriteTargetConfiguration(config *gateway.GatewayConfig, targets *targetpb.Configuration, file string) error {
//	f, err := os.Create(file)
//	if err != nil {
//		return fmt.Errorf("could not open configuration file %q: %v", file, err)
//	}
//	defer func() {
//		if err = f.Close(); err != nil {
//			config.Log.Error().Err(err).Msg("Error closing configuration file.")
//		}
//	}()
//
//	marshaler := jsonpb.Marshaler{Indent: "    "}
//	if err := marshaler.Marshal(f, targets); err != nil {
//		return fmt.Errorf("could not marshal configuration to JSON %q: %v", file, err)
//	}
//	if err = f.Close(); err != nil {
//		config.Log.Error().Err(err).Msg("Error closing configuration file.")
//	}
//	return nil
//}
