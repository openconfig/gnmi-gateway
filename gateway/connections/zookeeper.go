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
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi-gateway/gateway/cache"
	targetpb "github.com/openconfig/gnmi/proto/target"
	targetlib "github.com/openconfig/gnmi/target"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/locking"
)

var _ ConnectionManager = new(ZookeeperConnectionManager)

type ZookeeperConnectionManager struct {
	cache             *cache.Cache
	config            *configuration.GatewayConfig
	connLimit         *semaphore.Weighted
	connections       map[string]*ConnectionState
	connectionsMutex  sync.Mutex
	targetsConfigChan chan *TargetConnectionControl
	zkConn            *zk.Conn
}

// NewZookeeperConnectionManagerDefault creates a new ConnectionManager with an empty *cache.Cache.
// Locking will be enabled if zkConn is not nil.
func NewZookeeperConnectionManagerDefault(config *configuration.GatewayConfig, zkConn *zk.Conn, zkEvents <-chan zk.Event) (*ZookeeperConnectionManager, error) {
	mgr := ZookeeperConnectionManager{
		config:            config,
		connLimit:         semaphore.NewWeighted(int64(config.TargetLimit)),
		connections:       make(map[string]*ConnectionState),
		targetsConfigChan: make(chan *TargetConnectionControl, 10),
		zkConn:            zkConn,
	}
	mgr.cache = cache.New(nil)
	go mgr.eventListener(zkEvents)
	return &mgr, nil
}

func (c *ZookeeperConnectionManager) eventListener(zkEvents <-chan zk.Event) {
	for event := range zkEvents {
		switch event.State {
		case zk.StateDisconnected:
			c.config.Log.Info().Msgf("Zookeeper disconnected. Resetting locked target connections.")
			c.connectionsMutex.Lock()
			for _, targetConfig := range c.connections {
				if targetConfig.useLock {
					err := targetConfig.unlock()
					if err != nil {
						c.config.Log.Error().Msgf("error while unlocking target: %v", err)
					}
				}
			}
			c.connectionsMutex.Unlock()
		}
	}
}

func (c *ZookeeperConnectionManager) Cache() *cache.Cache {
	return c.cache
}

// Forwardable returns true if Notifications from the named target can be
// forwarded to Exporters.
func (c *ZookeeperConnectionManager) Forwardable(target string) bool {
	if target == "*" || target == "" {
		return true
	}
	c.connectionsMutex.Lock()
	defer c.connectionsMutex.Unlock()
	for _, conn := range c.connections {
		if conn.clusterMember {
			continue
		}

		if conn.useLock && !conn.ConnectionLockAcquired {
			continue
		}

		if conn.queryTarget == target || conn.Seen(target) {
			return true
		}

		if match, _ := filepath.Match(target, conn.queryTarget); match {
			return true
		}
	}
	return false
}

func (c *ZookeeperConnectionManager) TargetControlChan() chan<- *TargetConnectionControl {
	return c.targetsConfigChan
}

// ReloadTargets is a blocking loop to listen for target configurations from
// the TargetControlChan and handles connects/reconnects and disconnects to targets.
func (c *ZookeeperConnectionManager) ReloadTargets() {
	for targetControlMsg := range c.targetsConfigChan {
		c.handleTargetControlMsg(targetControlMsg)
	}
}

func (c *ZookeeperConnectionManager) handleTargetControlMsg(msg *TargetConnectionControl) {
	log.Info().Msgf("Connection manager received a target control message: %v inserts %v removes", msg.InsertCount(), msg.RemoveCount())

	if msg.Insert != nil {
		if err := targetlib.Validate(msg.Insert); err != nil {
			c.config.Log.Error().Err(err).Msgf("configuration is invalid: %v", err)
		}
	}

	c.connectionsMutex.Lock()
	// Disconnect from everything we want to remove
	for _, toRemove := range msg.Remove {
		conn, exists := c.connections[toRemove]
		if exists {
			err := conn.disconnect()
			if err != nil {
				c.config.Log.Warn().Msgf("error while disconnecting from target '%s': %v", toRemove, err)
			}
			delete(c.connections, toRemove)
		}
	}

	// Make new connections or update existing connections
	if msg.Insert != nil {
		for name, newConfig := range msg.Insert.Target {
			if existingConn, exists := c.connections[name]; exists {
				if !existingConn.Equal(newConfig) {
					// target is different; update the current config with the old one and reconnect
					c.config.Log.Info().Msgf("Updating connection for %s.", name)

					existingConn.target = newConfig
					existingConn.request = msg.Insert.Request[newConfig.Request]
					err := existingConn.reconnect()
					if err != nil {
						c.config.Log.Error().Err(err).Msgf("Error reconnecting to target: %s", name)
					}
				}
			} else {
				// no previous targetCache existed
				_, noLock := newConfig.Meta["NoLock"]
				c.config.Log.Info().Msgf("Initializing target %s (%v) %v. ; using lock: %v", name, newConfig.Addresses, newConfig.Meta, c.zkConn != nil && !noLock)
				_, clusterMember := newConfig.Meta["ClusterMember"]
				c.connections[name] = &ConnectionState{
					clusterMember: clusterMember,
					config:        c.config,
					connManager:   c,
					name:          name,
					targetCache:   c.cache.Add(name),
					target:        newConfig,
					request:       msg.Insert.Request[newConfig.Request],
					seen:          make(map[string]bool),
					useLock:       c.zkConn != nil && !noLock,
				}
				c.connections[name].InitializeMetrics()
				if c.connections[name].useLock {
					lockPath := MakeTargetLockPath(c.config.ZookeeperPrefix, name)
					clusterMemberAddress := c.config.ServerAddress + ":" + strconv.Itoa(c.config.ServerPort)
					c.connections[name].lock = locking.NewZookeeperNonBlockingLock(c.zkConn, lockPath, clusterMemberAddress, zk.WorldACL(zk.PermAll))
					go c.connections[name].connectWithLock(c.connLimit)
				} else {
					go c.connections[name].connect(c.connLimit)
				}
			}
		}
	}

	if msg.ReconnectAll {
		for _, conn := range c.connections {
			if err := conn.reconnect(); err != nil {
				c.config.Log.Error().Msgf("Recconect failed for connection: ", err)
			}
		}
	}

	c.connectionsMutex.Unlock()
}

func (c *ZookeeperConnectionManager) Start() error {
	go c.ReloadTargets()
	return nil
}

func MakeTargetLockPath(prefix string, target string) string {
	return strings.TrimRight(prefix, "/") + "/target/" + target
}

func (c *ZookeeperConnectionManager) GetTargetConfig(targetName string) (targetpb.Target, bool) {
	c.connectionsMutex.Lock()
	defer c.connectionsMutex.Unlock()

	connection, found := c.connections[targetName]
	var target targetpb.Target

	if !found {
		return target, false
	}
	if connection.target == nil {
		return target, false
	}
	target = *connection.target

	return target, true
}
