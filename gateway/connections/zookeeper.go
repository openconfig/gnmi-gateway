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
	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/locking"
	"github.com/openconfig/gnmi/cache"
	targetlib "github.com/openconfig/gnmi/target"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
	"strconv"
	"strings"
	"sync"
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
	for {
		select {
		case event := <-zkEvents:
			switch event.State {
			case zk.StateDisconnected:
				c.config.Log.Info().Msgf("Zookeeper disconnected. Resetting locked target connections.")
				c.connectionsMutex.Lock()
				for _, targetConfig := range c.connections {
					if targetConfig.useLock {
						_ = targetConfig.reconnect()
					}
				}
				c.connectionsMutex.Unlock()
			}
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
		if !conn.clusterMember && conn.ConnectionLockAcquired && conn.Seen(target) {
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
	for {
		select {
		case targetControlMsg := <-c.targetsConfigChan:
			log.Info().Msgf("Connection manager received a target control message: %v inserts %v removes", targetControlMsg.InsertCount(), targetControlMsg.RemoveCount())

			if targetControlMsg.Insert != nil {
				if err := targetlib.Validate(targetControlMsg.Insert); err != nil {
					c.config.Log.Error().Err(err).Msgf("configuration is invalid: %v", err)
				}
			}

			c.connectionsMutex.Lock()
			newConnections := make(map[string]*ConnectionState)
			for name, currentConfig := range c.connections {
				// Disconnect from everything we want to remove
				if stringInSlice(name, targetControlMsg.Remove) {
					err := currentConfig.disconnect()
					if err != nil {
						c.config.Log.Warn().Msgf("error while disconnecting from target '%s': %v", name, err)
					}
				}

				if targetControlMsg.Insert != nil {
					// Keep un-removed targets, check for changes in the insert list
					newConfig, exists := targetControlMsg.Insert.Target[name]
					if !exists {
						// no config for this target
						newConnections[name] = currentConfig
					} else {
						if !currentConfig.Equal(newConfig) {
							// target is different; update the current config with the old one and reconnect
							c.config.Log.Info().Msgf("Updating connection for %s.", name)

							currentConfig.target = newConfig
							currentConfig.request = targetControlMsg.Insert.Request[newConfig.Request]
							err := currentConfig.reconnect()
							if err != nil {
								c.config.Log.Error().Err(err).Msgf("Error reconnecting connection for %s", name)
							}
						}
						newConnections[name] = currentConfig
					}
				}
			}

			if targetControlMsg.Insert != nil {
				// make new connections
				for name, newConfig := range targetControlMsg.Insert.Target {
					if _, exists := newConnections[name]; exists {
						continue
					} else {
						// no previous targetCache existed
						c.config.Log.Info().Msgf("Initializing target %s (%v) %v.", name, newConfig.Addresses, newConfig.Meta)
						_, noLock := newConfig.Meta["NoLock"]
						_, clusterMember := newConfig.Meta["ClusterMember"]
						newConnections[name] = &ConnectionState{
							clusterMember: clusterMember,
							config:        c.config,
							connManager:   c,
							name:          name,
							targetCache:   c.cache.Add(name),
							target:        newConfig,
							request:       targetControlMsg.Insert.Request[newConfig.Request],
							seen:          make(map[string]bool),
							useLock:       c.zkConn != nil && !noLock,
						}
						newConnections[name].InitializeMetrics()
						if newConnections[name].useLock {
							lockPath := MakeTargetLockPath(c.config.ZookeeperPrefix, name)
							clusterMemberAddress := c.config.ServerAddress + ":" + strconv.Itoa(c.config.ServerPort)
							newConnections[name].lock = locking.NewZookeeperNonBlockingLock(c.zkConn, lockPath, clusterMemberAddress, zk.WorldACL(zk.PermAll))
							go newConnections[name].connectWithLock(c.connLimit)
						} else {
							go newConnections[name].connect(c.connLimit)
						}
					}
				}
			}

			// save the target changes
			c.connections = newConnections
			c.connectionsMutex.Unlock()
		}
	}
}

func (c *ZookeeperConnectionManager) Start() error {
	go c.ReloadTargets()
	return nil
}

func MakeTargetLockPath(prefix string, target string) string {
	return strings.TrimRight(prefix, "/") + "/target/" + target
}

func stringInSlice(s string, list []string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
