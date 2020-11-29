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

// Package simple provides a TargetLoader for parsing a simple target YAML
// config file. Here is an example simple config:
//	---
//	connection:
//	  my-router:
//	    addresses:
//	      - my-router.test.example.net:9339
//      credentials:
//		  username: myusername
//		  password: mypassword
//	    request: my-request
//	    meta: {}
//	request:
//	  my-request:
//	    target: *
//	    paths:
//	      - /components
//	      - /interfaces/interface[name=*]/state/counters
//	      - /qos/interfaces/interface[interface-id=*]/output/queues/queue[name=*]/state
package simple

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/google/gnxi/utils/xpath"
	"github.com/openconfig/gnmi/proto/gnmi"
	targetpb "github.com/openconfig/gnmi/proto/target"
	"github.com/openconfig/gnmi/target"
	"gopkg.in/yaml.v2"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/loaders"
)

var _ loaders.TargetLoader = new(SimpleTargetLoader)

type TargetConfig struct {
	Connection map[string]ConnectionConfig `yaml:"connection"`
	Request    map[string]RequestConfig    `yaml:"request"`
}

type ConnectionConfig struct {
	Addresses   []string          `yaml:"addresses"`
	Request     string            `yaml:"request"`
	Meta        map[string]string `yaml:"meta"`
	Credentials CredentialsConfig `yaml:"credentials"`
}

type RequestConfig struct {
	Target string   `yaml:"target"`
	Paths  []string `yaml:"paths"`
}

type CredentialsConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type SimpleTargetLoader struct {
	config   *configuration.GatewayConfig
	file     string
	interval time.Duration
	last     *targetpb.Configuration
}

func init() {
	loaders.Register("simple", NewSimpleTargetLoader)
}

func NewSimpleTargetLoader(config *configuration.GatewayConfig) loaders.TargetLoader {
	return &SimpleTargetLoader{
		config:   config,
		file:     config.TargetLoaders.SimpleFile,
		interval: config.TargetLoaders.SimpleFileReloadInterval,
	}
}

func (m *SimpleTargetLoader) GetConfiguration() (*targetpb.Configuration, error) {
	data, err := ioutil.ReadFile(m.file)
	if err != nil {
		return nil, fmt.Errorf("could not open simple config file %q: %v", m.file, err)
	}

	configs, err := m.yamlToTargets(&data)
	if err != nil {
		return nil, err
	}

	if err := target.Validate(configs); err != nil {
		return nil, fmt.Errorf("configuration in %q is invalid: %v", m.file, err)
	}
	return configs, nil
}

func (m *SimpleTargetLoader) yamlToTargets(data *[]byte) (*targetpb.Configuration, error) {
	var simpleConfig TargetConfig
	if err := yaml.Unmarshal(*data, &simpleConfig); err != nil {
		return nil, fmt.Errorf("could not parse configuration from %q: %v", m.file, err)
	}

	configs := &targetpb.Configuration{
		Target:  make(map[string]*targetpb.Target),
		Request: make(map[string]*gnmi.SubscribeRequest),
	}
	for requestName, request := range simpleConfig.Request {
		var subs []*gnmi.Subscription
		for _, x := range request.Paths {
			path, err := xpath.ToGNMIPath(x)
			if err != nil {
				return nil, fmt.Errorf("unable to parse simple config XPath: %s: %v", x, err)
			}
			subs = append(subs, &gnmi.Subscription{Path: path})
		}
		configs.Request[requestName] = &gnmi.SubscribeRequest{
			Request: &gnmi.SubscribeRequest_Subscribe{
				Subscribe: &gnmi.SubscriptionList{
					Prefix: &gnmi.Path{
						Target: request.Target,
					},
					Subscription: subs,
				},
			},
		}
	}

	for connName, conn := range simpleConfig.Connection {
		configs.Target[connName] = &targetpb.Target{
			Addresses: conn.Addresses,
			Request:   conn.Request,
			Meta:      conn.Meta,
			Credentials: &targetpb.Credentials{
				Username: conn.Credentials.Username,
				Password: conn.Credentials.Password,
			},
		}
	}
	return configs, nil
}

func (m *SimpleTargetLoader) Start() error {
	_, err := m.GetConfiguration() // make sure there are no errors at startup
	return err
}

func (m *SimpleTargetLoader) WatchConfiguration(targetChan chan<- *connections.TargetConnectionControl) error {
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
