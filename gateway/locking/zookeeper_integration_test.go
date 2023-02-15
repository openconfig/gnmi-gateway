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

package locking_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/stretchr/testify/assert"

	"github.com/mspiez/gnmi-gateway/gateway/locking"
)

const ZookeeperIntegrationServerAddress = "127.0.0.1:2181"
const ZookeeperIntegrationServerTimeout = 2 * time.Second
const ZookeeperIntegrationTestID = "test-id-12345"
const ZookeeperIntegrationTestMember = "127.0.0.1:2181"

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

func TestZookeeperNonBlockingLock_Try(t *testing.T) {
	assertion := assert.New(t)

	conn, err := connectToZK()
	assertion.NoError(err)

	lock := locking.NewZookeeperNonBlockingLock(conn, ZookeeperIntegrationTestID, ZookeeperIntegrationTestMember, zk.WorldACL(zk.PermAll))
	assertion.NotNil(lock)

	// Lock works
	acquired, err := lock.Try()
	assertion.NoError(err)
	assertion.True(acquired)

	// Deadlock error
	acquired, err = lock.Try()
	assertion.Error(err, "should have a deadlock error")
	assertion.True(acquired)

	// GetMember works
	member, err := lock.GetMember(ZookeeperIntegrationTestID)
	assertion.NoError(err)
	assertion.Equal(ZookeeperIntegrationTestMember, member)

	// Unlock works
	err = lock.Unlock()
	assertion.NoError(err)

	// GetMember is empty
	member, err = lock.GetMember(ZookeeperIntegrationTestID)
	assertion.NoError(err)
	assertion.Empty(member)

	// Unlock error
	err = lock.Unlock()
	assertion.Error(err, "should not be able to unlock an un-acquired lock")

	conn.Close()
}

func TestZookeeperNonBlockingLock_GetMember(t *testing.T) {
	assertion := assert.New(t)

	conn, err := connectToZK()
	assertion.NoError(err)

	lock_one := locking.NewZookeeperNonBlockingLock(conn, ZookeeperIntegrationTestID, ZookeeperIntegrationTestMember, zk.WorldACL(zk.PermAll))
	assertion.NotNil(lock_one)

	lock_two := locking.NewZookeeperNonBlockingLock(conn, ZookeeperIntegrationTestID, strReverse(ZookeeperIntegrationTestMember), zk.WorldACL(zk.PermAll))
	assertion.NotNil(lock_two)

	acquired, err := lock_two.Try()
	assertion.NoError(err)
	assertion.True(acquired)

	acquired, err = lock_one.Try()
	assertion.NoError(err)
	assertion.False(acquired)

	member, err := lock_one.GetMember(ZookeeperIntegrationTestID)
	assertion.NoError(err)
	assertion.Equal(strReverse(ZookeeperIntegrationTestMember), member)

	conn.Close()
}

func strReverse(in string) string {
	out := ""
	for i := range in {
		out += string(in[len(in)-i-1])
	}
	return out
}
