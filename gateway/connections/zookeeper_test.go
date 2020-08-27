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

package connections

import (
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi/client"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewZookeeperConnectionManagerDefault(t *testing.T) {
	assertion := assert.New(t)

	mgr, err := NewZookeeperConnectionManagerDefault(&configuration.GatewayConfig{}, nil, nil)
	assertion.NoError(err)
	assertion.NotNil(mgr)
}

func TestZookeeperConnectionManager_handleTargetControlMsg_Delete(t *testing.T) {
	assertion := assert.New(t)

	mgr, err := NewZookeeperConnectionManagerDefault(&configuration.GatewayConfig{}, nil, nil)
	assertion.NoError(err)
	assertion.NotNil(mgr)

	// Initialize with some connections
	mgr.connections = map[string]*ConnectionState{
		"one":   {config: new(configuration.GatewayConfig), client: client.Reconnect(&client.BaseClient{}, nil, nil)},
		"two":   {config: new(configuration.GatewayConfig), client: client.Reconnect(&client.BaseClient{}, nil, nil)},
		"three": {config: new(configuration.GatewayConfig), client: client.Reconnect(&client.BaseClient{}, nil, nil)},
	}

	// Attempt to remove a single connection
	mgr.handleTargetControlMsg(&TargetConnectionControl{Remove: []string{"two"}})
	assertion.Len(mgr.connections, 2)
	assertion.NotNil(mgr.connections["one"])
	assertion.NotNil(mgr.connections["three"])

	// Attempt to remove a non-existent connection
	mgr.handleTargetControlMsg(&TargetConnectionControl{Remove: []string{"eleven"}})
	assertion.Len(mgr.connections, 2)
	assertion.NotNil(mgr.connections["one"])
	assertion.NotNil(mgr.connections["three"])

	// Attempt to remove a single connection
	mgr.handleTargetControlMsg(&TargetConnectionControl{Remove: []string{"one"}})
	assertion.Len(mgr.connections, 1)
	assertion.NotNil(mgr.connections["three"])

	// Attempt to remove a single connection
	mgr.handleTargetControlMsg(&TargetConnectionControl{Remove: []string{"three"}})
	assertion.Len(mgr.connections, 0)
	assertion.Nil(mgr.connections["three"])

	// Attempt to remove from empty connection map
	mgr.handleTargetControlMsg(&TargetConnectionControl{Remove: []string{"three"}})
	assertion.Len(mgr.connections, 0)
	assertion.Nil(mgr.connections["three"])
}
