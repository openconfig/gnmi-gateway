//go:build integration
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
	"sync"
	"testing"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/stretchr/testify/assert"

	"github.com/mspiez/gnmi-gateway/gateway/clustering"
	"github.com/mspiez/gnmi-gateway/gateway/configuration"
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

	cluster := clustering.NewZookeeperClusterMember(config, conn, ZookeeperIntegrationTestMemberOne)
	assertion.NoError(cluster.Register())

	members, err := cluster.MemberList()
	assertion.NoError(err)
	assertion.Len(members, 0)

	conn.Close()
}

// TODO (cmcintosh): enable this test
//func TestZookeeperCluster_EmptyMemberList(t *testing.T) {
//	assertion := assert.New(t)
//
//	config := minimalGatewayConfig()
//	conn, err := connectToZK()
//	assertion.NoError(err)
//
//	searchPath := clustering.CleanPath(config.ZookeeperPrefix) + clustering.CleanPath(clustering.ClusterMemberPath)
//
//	err = conn.Delete(searchPath, 0)
//	if err != nil && err != zk.ErrNoNode {
//		assertion.NoError(fmt.Errorf("unable to delete test node: %v", err))
//	}
//
//	cluster := clustering.NewZookeeperClusterMember(config, conn, ZookeeperIntegrationTestMemberOne)
//	members, err := cluster.MemberList()
//	assertion.NoError(err)
//	assertion.Len(members, 0)
//
//	conn.Close()
//}

func TestZookeeperCluster_EmptyMemberListCallback(t *testing.T) {
	assertion := assert.New(t)

	config := minimalGatewayConfig()
	conn, err := connectToZK()
	assertion.NoError(err)

	searchPath := clustering.CleanPath(config.ZookeeperPrefix) + clustering.CleanPath(clustering.ClusterMemberPath)

	err = conn.Delete(searchPath, 0)
	if err != nil && err != zk.ErrNoNode {
		assertion.NoError(fmt.Errorf("unable to delete test node: %v", err))
	}

	cluster := clustering.NewZookeeperClusterMember(config, conn, ZookeeperIntegrationTestMemberOne)
	assertion.NoError(cluster.MemberListCallback(func(add clustering.MemberID, remove clustering.MemberID) {}))

	conn.Close()
}

func TestZookeeperCluster_RegisterDuplicate(t *testing.T) {
	assertion := assert.New(t)

	config := minimalGatewayConfig()
	conn, err := connectToZK()
	assertion.NoError(err)

	cluster := clustering.NewZookeeperClusterMember(config, conn, ZookeeperIntegrationTestMemberOne)
	assertion.NoError(cluster.Register())
	assertion.Error(cluster.Register())

	members, err := cluster.MemberList()
	assertion.NoError(err)
	assertion.Len(members, 0)

	conn.Close()
}

func TestZookeeperCluster_MemberChangeCallback(t *testing.T) {
	assertion := assert.New(t)

	config := minimalGatewayConfig()
	connOne, err := connectToZK()
	assertion.NoError(err)

	connTwo, err := connectToZK()
	assertion.NoError(err)

	clusterOne := clustering.NewZookeeperClusterMember(config, connOne, ZookeeperIntegrationTestMemberOne)
	clusterTwo := clustering.NewZookeeperClusterMember(config, connTwo, ZookeeperIntegrationTestMemberTwo)

	wg := new(sync.WaitGroup)
	wg.Add(1)
	assertion.NoError(clusterOne.Register())

	var gotMemberOneAdd = false
	var gotMemberTwoAdd = false
	var gotMemberOneRemove = false
	assertion.NoError(clusterTwo.MemberListCallback(func(add clustering.MemberID, remove clustering.MemberID) {
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

	// this shouldn't trigger the callback for itself
	assertion.NoError(clusterTwo.Register())

	wg.Add(1)
	connOne.Close() // should remove the nodes for member one

	wg.Wait()
	assertion.True(gotMemberOneAdd)
	assertion.False(gotMemberTwoAdd)
	assertion.True(gotMemberOneRemove)

	connTwo.Close()
}
