// Copyright 2020 Netflix Inc
// Author: Colin McIntosh (colin@netflix.com)

package targets

import (
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	targetpb "github.com/openconfig/gnmi/proto/target"
	"github.com/openconfig/gnmi/target"
	"os"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway"
	"time"
)

type JSONFileTargetLoader struct {
	config   *gateway.GatewayConfig
	file     string
	interval time.Duration
}

func NewJSONFileTargetLoader(config *gateway.GatewayConfig) TargetLoader {
	return &JSONFileTargetLoader{
		config:   config,
		file:     config.TargetConfigurationJSONFile,
		interval: config.TargetConfigurationJSONFileReloadInterval,
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
	configuration := new(targetpb.Configuration)
	if err := jsonpb.Unmarshal(f, configuration); err != nil {
		return nil, fmt.Errorf("could not parse configuration from %q: %v", m.file, err)
	}
	if err := target.Validate(configuration); err != nil {
		return nil, fmt.Errorf("configuration in %q is invalid: %v", m.file, err)
	}
	return configuration, nil
}

func (m *JSONFileTargetLoader) WatchConfiguration(targetChan chan *targetpb.Configuration) {
	for {
		targetConfig, err := m.GetConfiguration()
		if err != nil {
			m.config.Log.Error().Err(err).Msgf("Unable to get target configuration.")
		} else {
			targetChan <- targetConfig
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
