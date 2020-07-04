// +build integration

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

package clustering_test

import (
	"fmt"
	"github.com/samuel/go-zookeeper/zk"
	"github.com/stretchr/testify/assert"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/clustering"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"sync"
	"testing"
	"time"
)

const ZookeeperIntegrationServerAddress = "127.0.0.1:2181"
const ZookeeperIntegrationServerTimeout = 2 * time.Second
const ZookeeperIntegrationTestMemberOne = "127.0.0.1:2181"
const ZookeeperIntegrationTestMemberTwo = "127.0.0.2:2181"

type ZKLogger struct{}

func (z *ZKLogger) Printf(a string, b ...interface{}) {
	fmt.Println("Zookeeper:", a, b)
}

func connectToZK() (*zk.Conn, error) {
	newConn, _, err := zk.Connect([]string{ZookeeperIntegrationServerAddress}, ZookeeperIntegrationServerTimeout, zk.WithLogger(new(ZKLogger)))
	if err != nil {
		return nil, fmt.Errorf("unable to connect to Zookeeper integration server '%s': %v", ZookeeperIntegrationServerAddress, err)
	}
	// wait for the connection to establish
	start := time.Now()
	var state zk.State
	for time.Now().Before(start.Add(ZookeeperIntegrationServerTimeout)) {
		state = newConn.State()
		if state == zk.StateHasSession || state == zk.StateConnected {
			return newConn, nil
		}
	}
	return newConn, fmt.Errorf("timeout connecting to Zookeeper integration server '%s': %v", ZookeeperIntegrationServerAddress, state)
}

func minimalGatewayConfig() *configuration.GatewayConfig {
	return &configuration.GatewayConfig{
		ZookeeperPrefix:  "/gnmi/testgateway",
		ZookeeperTimeout: 1 * time.Second,
	}
}

func TestZookeeperCluster_Register(t *testing.T) {
	assertion := assert.New(t)

	config := minimalGatewayConfig()
	conn, err := connectToZK()
	assertion.NoError(err)

	cluster := clustering.NewZookeeperCluster(config, conn)
	assertion.NoError(cluster.Register(ZookeeperIntegrationTestMemberOne))

	members, err := cluster.MemberList()
	assertion.NoError(err)
	assertion.Len(members, 1)
	assertion.Equal([]string{ZookeeperIntegrationTestMemberOne}, members)

	conn.Close()
}

func TestZookeeperCluster_RegisterMultiple(t *testing.T) {
	assertion := assert.New(t)

	config := minimalGatewayConfig()
	conn, err := connectToZK()
	assertion.NoError(err)

	cluster := clustering.NewZookeeperCluster(config, conn)
	assertion.NoError(cluster.Register(ZookeeperIntegrationTestMemberOne))
	assertion.NoError(cluster.Register(ZookeeperIntegrationTestMemberTwo))

	members, err := cluster.MemberList()
	assertion.NoError(err)
	assertion.Len(members, 2)
	assertion.Contains(members, ZookeeperIntegrationTestMemberOne)
	assertion.Contains(members, ZookeeperIntegrationTestMemberTwo)

	conn.Close()
}

func TestZookeeperCluster_RegisterDuplicate(t *testing.T) {
	assertion := assert.New(t)

	config := minimalGatewayConfig()
	conn, err := connectToZK()
	assertion.NoError(err)

	cluster := clustering.NewZookeeperCluster(config, conn)
	assertion.NoError(cluster.Register(ZookeeperIntegrationTestMemberOne))
	assertion.Error(cluster.Register(ZookeeperIntegrationTestMemberOne))

	members, err := cluster.MemberList()
	assertion.NoError(err)
	assertion.Len(members, 1)
	assertion.Equal([]string{ZookeeperIntegrationTestMemberOne}, members)

	conn.Close()
}

func TestZookeeperCluster_MemberChangeCallback(t *testing.T) {
	assertion := assert.New(t)

	config := minimalGatewayConfig()
	connOne, err := connectToZK()
	assertion.NoError(err)

	connTwo, err := connectToZK()
	assertion.NoError(err)

	clusterOne := clustering.NewZookeeperCluster(config, connOne)
	clusterTwo := clustering.NewZookeeperCluster(config, connTwo)

	wg := new(sync.WaitGroup)
	wg.Add(1)
	assertion.NoError(clusterOne.Register(ZookeeperIntegrationTestMemberOne))

	var gotMemberOneAdd = false
	var gotMemberTwoAdd = false
	var gotMemberOneRemove = false
	assertion.NoError(clusterTwo.MemberChangeCallback(func(add string, remove string) {
		if add != "" {
			if add == ZookeeperIntegrationTestMemberOne {
				gotMemberOneAdd = true
			} else if add == ZookeeperIntegrationTestMemberTwo {
				gotMemberTwoAdd = true
			}
		}
		if remove != "" {
			if remove == ZookeeperIntegrationTestMemberOne {
				gotMemberOneRemove = true
			}
		}
		wg.Done()
	}))

	wg.Add(1)
	assertion.NoError(clusterTwo.Register(ZookeeperIntegrationTestMemberTwo))

	wg.Add(1)
	connOne.Close() // should remove the nodes for member one

	wg.Wait()
	assertion.True(gotMemberOneAdd)
	assertion.True(gotMemberTwoAdd)
	assertion.True(gotMemberOneRemove)

	connTwo.Close()
}
