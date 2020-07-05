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

// Package connections provides the connection manager that forms the transport connection
// to gNMI targets and initiates gNMI RPC calls.
//
// The ConnectionManager listens for TargetConnectionControl messages on the TargetControlChan
// and will perform the corresponding connects and disconnects for Insert and Remove, respectively.
// When an Insert is received the ConnectionManager will attempt to connect to the target if there
// is a connection slot available. The number of connection slots is configurable with the
// TargetLimit configuration parameter. If clustering is enabled the connection manager
// will attempt to acquire a lock for each target before making the connection. Locking will not be
// attempted if there are no connection slots available. Targets present in the Insert field will
// disconnect and overwrite existing targets with the same name, if the target configuration has
// changed.
//
// The ConnectionManager accepts target configurations that follow the target.proto found here:
// https://github.com/openconfig/gnmi/blob/master/proto/target/target.proto
// The ConnectionManager additionally supports some per-target meta configuration options:
//		NoTLS	- Set this field to disable TLS for the target. If client TLS credentials
//				  are not provided this field will have no effect.
//		NoLock	- Set this field to disable locking for the target. If clustering is not
//				  enabled this field will have no effect.
package connections

import (
	"github.com/openconfig/gnmi/cache"
	targetpb "github.com/openconfig/gnmi/proto/target"
	targetlib "github.com/openconfig/gnmi/target"
	"github.com/rs/zerolog/log"
	"github.com/samuel/go-zookeeper/zk"
	"golang.org/x/sync/semaphore"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/locking"
	"strconv"
	"strings"
	"sync"
)

// NewConnectionManagerDefault creates a new ConnectionManager with an empty *cache.Cache.
// Start must be called to start listening for changes on the TargetControlChan.
// Locking will be enabled if zkConn is not nil.
func NewConnectionManagerDefault(config *configuration.GatewayConfig, zkConn *zk.Conn) (*ConnectionManager, error) {
	mgr := ConnectionManager{
		config:            config,
		connLimit:         semaphore.NewWeighted(int64(config.TargetLimit)),
		targets:           make(map[string]*TargetState),
		targetsConfigChan: make(chan *TargetConnectionControl, 10),
		zkConn:            zkConn,
	}
	cache.Type = cache.GnmiNoti
	mgr.cache = cache.New(nil)
	return &mgr, nil
}

// TargetConnectionControl messages are used to insert/update and remove targets in
// the ConnectionManager via the TargetControlChan channel.
type TargetConnectionControl struct {
	// Insert will insert and connect all of the named target configs. If a config with the same name
	// already exists it will be overwritten and the target reconnected if the new config is different
	// from the old one.
	Insert *targetpb.Configuration
	// Remove will remove and disconnect from all of the named targets.
	Remove []string
}

// InsertCount is the number of targets in the Insert field, if Insert is not nil.
func (t *TargetConnectionControl) InsertCount() int {
	if t.Insert == nil || t.Insert.Target == nil {
		return 0
	}
	return len(t.Insert.Target)
}

// RemoveCount is the number of targets in the Remove field, if Remove is not nil.
func (t *TargetConnectionControl) RemoveCount() int {
	return len(t.Remove)
}

type ConnectionManager struct {
	cache             *cache.Cache
	config            *configuration.GatewayConfig
	connLimit         *semaphore.Weighted
	targets           map[string]*TargetState
	targetsMutex      sync.Mutex
	targetsConfigChan chan *TargetConnectionControl
	zkConn            *zk.Conn
}

// Cache returns the *cache.Cache that contains gNMI Notifications.
func (c *ConnectionManager) Cache() *cache.Cache {
	return c.cache
}

// HasTargetLock returns true if this instance of the ConnectionManager holds
// the lock for the named target.
func (c *ConnectionManager) HasTargetLock(target string) bool {
	c.targetsMutex.Lock()
	targetState, exists := c.targets[target]
	c.targetsMutex.Unlock()
	if !exists || !targetState.ConnectionLockAcquired {
		return false
	}
	return true
}

// TargetControlChan returns an input channel for TargetConnectionControl messages.
func (c *ConnectionManager) TargetControlChan() chan<- *TargetConnectionControl {
	return c.targetsConfigChan
}

// ReloadTargets is a blocking loop to listen for target configurations from
// the TargetControlChan and handles connects/reconnects and disconnects to targets.
func (c *ConnectionManager) ReloadTargets() {
	for {
		select {
		case targetControlMsg := <-c.targetsConfigChan:
			log.Info().Msgf("Connection manager received a target control message: %v inserts %v removes", targetControlMsg.InsertCount(), targetControlMsg.RemoveCount())

			if targetControlMsg.Insert != nil {
				if err := targetlib.Validate(targetControlMsg.Insert); err != nil {
					c.config.Log.Error().Err(err).Msgf("configuration is invalid: %v", err)
				}
			}

			c.targetsMutex.Lock()
			newTargets := make(map[string]*TargetState)
			for name, currentConfig := range c.targets {
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
						newTargets[name] = currentConfig
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
						newTargets[name] = currentConfig
					}
				}
			}

			if targetControlMsg.Insert != nil {
				// make new connections
				for name, newConfig := range targetControlMsg.Insert.Target {
					if _, exists := newTargets[name]; exists {
						continue
					} else {
						// no previous targetCache existed
						c.config.Log.Info().Msgf("Initializing target %s (%v) %v.", name, newConfig.Addresses, newConfig.Meta)
						newTargets[name] = &TargetState{
							config:      c.config,
							connManager: c,
							name:        name,
							targetCache: c.cache.Add(name),
							target:      newConfig,
							request:     targetControlMsg.Insert.Request[newConfig.Request],
						}

						_, noLock := newConfig.Meta["NoLock"]
						if c.zkConn == nil || noLock {
							go newTargets[name].connect(c.connLimit)
						} else {
							lockPath := MakeTargetLockPath(c.config.ZookeeperPrefix, name)
							clusterMemberAddress := c.config.ServerAddress + ":" + strconv.Itoa(c.config.ServerPort)
							newTargets[name].lock = locking.NewZookeeperNonBlockingLock(c.zkConn, lockPath, clusterMemberAddress, zk.WorldACL(zk.PermAll))
							go newTargets[name].connectWithLock(c.connLimit)
						}
					}
				}
			}

			// save the target changes
			c.targets = newTargets
			c.targetsMutex.Unlock()
		}
	}
}

// Start will start the loop to listen for TargetConnectionControl messages on TargetControlChan.
func (c *ConnectionManager) Start() error {
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
