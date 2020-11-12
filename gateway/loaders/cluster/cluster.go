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

package cluster

import (
	"fmt"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/proto/target"

	"github.com/openconfig/gnmi-gateway/gateway/clustering"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
)

// ClusterTargetLoader is used internally to connect/disconnect from other cluster members if clustering is enabled.
type ClusterTargetLoader struct {
	config  *configuration.GatewayConfig
	cluster clustering.ClusterMember
}

func NewClusterTargetLoader(config *configuration.GatewayConfig, cluster clustering.ClusterMember) *ClusterTargetLoader {
	return &ClusterTargetLoader{
		config:  config,
		cluster: cluster,
	}
}

func (c ClusterTargetLoader) GetConfiguration() (*target.Configuration, error) {
	memberList, err := c.cluster.MemberList()
	if err != nil {
		return nil, fmt.Errorf("unable to get member list to generate target configuration: %v", err)
	}

	targetConfig := &target.Configuration{
		Request: map[string]*gnmi.SubscribeRequest{
			"all": {
				Request: &gnmi.SubscribeRequest_Subscribe{
					Subscribe: &gnmi.SubscriptionList{
						Prefix: &gnmi.Path{
							Target: "*",
						},
						Subscription: []*gnmi.Subscription{
							{
								Path: &gnmi.Path{
									Elem: []*gnmi.PathElem{},
								},
							},
						},
					},
				},
			},
		},
		Target: map[string]*target.Target{},
	}

	for _, member := range memberList {
		memberAddress := string(member)
		targetConfig.Target[memberAddress] = &target.Target{
			Addresses:   []string{memberAddress},
			Credentials: nil,
			Request:     "all",
		}
	}
	return targetConfig, nil
}

func (c ClusterTargetLoader) Start() error {
	return nil // nothing to start
}

func (c ClusterTargetLoader) WatchConfiguration(configChan chan<- *connections.TargetConnectionControl) error {
	err := c.cluster.MemberListCallback(func(add clustering.MemberID, remove clustering.MemberID) {
		if add != "" {
			c.config.Log.Info().Msgf("Cluster Loader: Add Member: %s", add)
			configChan <- &connections.TargetConnectionControl{
				Insert: &target.Configuration{
					Request: map[string]*gnmi.SubscribeRequest{
						"all": {
							Request: &gnmi.SubscribeRequest_Subscribe{
								Subscribe: &gnmi.SubscriptionList{
									Prefix: &gnmi.Path{
										Target: "*",
									},
									Subscription: []*gnmi.Subscription{
										{
											Path: &gnmi.Path{
												Elem: []*gnmi.PathElem{},
											},
										},
									},
								},
							},
						},
					},
					Target: map[string]*target.Target{
						string(add): {
							Addresses:   []string{string(add)},
							Credentials: nil,
							Request:     "all",
							Meta: map[string]string{
								"NoLock":        "yes",
								"ClusterMember": "yes",
							},
						},
					},
				},
			}
		}
		if remove != "" {
			c.config.Log.Info().Msgf("Cluster Loader: Remove Member: %s", remove)
			configChan <- &connections.TargetConnectionControl{
				Remove: []string{string(remove)},
			}
		}
	})
	return err
}
