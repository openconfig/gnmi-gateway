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

// Package netbox provides a TargetLoader for loading devices from NetBox.
package netbox

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/google/gnxi/utils/xpath"
	"github.com/netbox-community/go-netbox/netbox/client"
	"github.com/netbox-community/go-netbox/netbox/client/dcim"
	"github.com/openconfig/gnmi/proto/gnmi"
	targetpb "github.com/openconfig/gnmi/proto/target"
	"github.com/openconfig/gnmi/target"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/loaders"
)

const Name = "netbox"

var _ loaders.TargetLoader = new(NetBoxTargetLoader)

type NetBoxTargetLoader struct {
	config   *configuration.GatewayConfig
	last     *targetpb.Configuration
	apiKey   string
	client   *client.NetBoxAPI
	host     string
	interval time.Duration
}

func init() {
	loaders.Register(Name, NewNetBoxTargetLoader)
}

func NewNetBoxTargetLoader(config *configuration.GatewayConfig) loaders.TargetLoader {
	return &NetBoxTargetLoader{
		config:   config,
		apiKey:   config.TargetLoaders.NetBoxAPIKey,
		host:     config.TargetLoaders.NetBoxHost,
		interval: config.TargetLoaders.NetBoxReloadInterval,
	}
}

func (m *NetBoxTargetLoader) GetConfiguration() (*targetpb.Configuration, error) {
	resp, err := m.client.Dcim.DcimDevicesList(&dcim.DcimDevicesListParams{
		Context: context.Background(),
		Tag:     &m.config.TargetLoaders.NetBoxIncludeTag,
	}, nil)
	if err != nil {
		if resp != nil {
			m.config.Log.Error().Msgf("NetBox devices list response: %s", resp.Error())
		}
		err = fmt.Errorf("unable to list devices in NetBox with tag '%s': %v", m.config.TargetLoaders.NetBoxIncludeTag, err)
		m.config.Log.Error().Msg(err.Error())
		return nil, err
	}

	configs := &targetpb.Configuration{
		Target:  make(map[string]*targetpb.Target),
		Request: make(map[string]*gnmi.SubscribeRequest),
	}

	var subs []*gnmi.Subscription
	for _, x := range m.config.TargetLoaders.NetBoxSubscribePaths {
		path, err := xpath.ToGNMIPath(x)
		if err != nil {
			return nil, fmt.Errorf("unable to parse simple config XPath: %s: %v", x, err)
		}
		subs = append(subs, &gnmi.Subscription{Path: path})
	}
	configs.Request["default"] = &gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Subscribe{
			Subscribe: &gnmi.SubscriptionList{
				Prefix:       &gnmi.Path{},
				Subscription: subs,
			},
		},
	}

	payload := resp.GetPayload()
	if payload != nil {
		for _, device := range payload.Results {
			if device.PrimaryIP.Address == nil || *device.PrimaryIP.Address == "" {
				continue
			}
			ip, _, err := net.ParseCIDR(*device.PrimaryIP.Address)
			if err != nil {
				m.config.Log.Error().Msgf("unable to parse IP for NetBox device %s: %v", device.Name, err)
				continue
			}

			ipBytes, _ := ip.MarshalText()
			address := string(ipBytes) + ":" + strconv.Itoa(m.config.TargetLoaders.NetBoxDeviceGNMIPort)

			configs.Target[*device.Name] = &targetpb.Target{
				Addresses: []string{address},
				Request:   "default",
				Credentials: &targetpb.Credentials{
					Username: m.config.TargetLoaders.NetBoxDeviceUsername,
					Password: m.config.TargetLoaders.NetBoxDevicePassword,
				},
			}
		}
	}

	if err := target.Validate(configs); err != nil {
		return nil, fmt.Errorf("configuration from NetBox loader is invalid: %w", err)
	}
	return configs, nil
}

func (m *NetBoxTargetLoader) Start() error {
	var scheme = "https"
	var transport *httptransport.Runtime
	var basePath = client.DefaultBasePath

	if m.config.TargetLoaders.Insecure {
		scheme = "http"
	}

	if len(m.config.TargetLoaders.NetBoxBasePath) > 0 {
		basePath = m.config.TargetLoaders.NetBoxBasePath
	}

	if !m.config.TargetLoaders.Insecure && m.config.TargetLoaders.NoTLSVerify {
		tlsClient, err := httptransport.TLSClient(httptransport.TLSClientOptions{InsecureSkipVerify: true})
		if err != nil {
			return err
		}
		transport = httptransport.NewWithClient(m.config.TargetLoaders.NetBoxHost, basePath, []string{scheme}, tlsClient)
	} else {
		transport = httptransport.New(m.config.TargetLoaders.NetBoxHost, basePath, []string{scheme})
	}

	transport.DefaultAuthentication = httptransport.APIKeyAuth("Authorization", "header", "Token "+m.config.TargetLoaders.NetBoxAPIKey)
	m.client = client.New(transport, nil)

	_, err := m.GetConfiguration() // make sure there are no errors at startup
	return err
}

func (m *NetBoxTargetLoader) WatchConfiguration(targetChan chan<- *connections.TargetConnectionControl) error {
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
