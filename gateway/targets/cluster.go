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

package targets

import (
	"github.com/openconfig/gnmi/proto/target"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/clustering"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
)

// ClusterTargetLoader is used internally to connect/disconnect from other cluster members if clustering is enabled.
type ClusterTargetLoader struct {
	config  *configuration.GatewayConfig
	cluster clustering.Cluster
}

func NewClusterTargetLoader(config *configuration.GatewayConfig, cluster clustering.Cluster) *ClusterTargetLoader {
	return &ClusterTargetLoader{
		config:  config,
		cluster: cluster,
	}
}

func (c ClusterTargetLoader) GetConfiguration() (*target.Configuration, error) {
	panic("implement me")
}

func (c ClusterTargetLoader) Start() error {
	panic("implement me")
}

func (c ClusterTargetLoader) WatchConfiguration(chan *target.Configuration) {
	panic("implement me")
}
