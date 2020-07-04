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

type TargetConnectionControl struct {
	// Insert will insert and connect all of the named target configs. If a config with the same name
	// already exists it will be overwritten and the target reconnected if the new config is different
	// from the old one.
	Insert *targetpb.Configuration
	// Remove will remove and disconnect from all of the named targets.
	Remove []string
}

func (t *TargetConnectionControl) InsertCount() int {
	if t.Insert == nil || t.Insert.Target == nil {
		return 0
	}
	return len(t.Insert.Target)
}

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

func (c *ConnectionManager) Cache() *cache.Cache {
	return c.cache
}

func (c *ConnectionManager) HasLocalTargetLock(target string) bool {
	c.targetsMutex.Lock()
	targetState, exists := c.targets[target]
	c.targetsMutex.Unlock()
	if !exists || !targetState.ConnectionLockAcquired {
		return false
	}
	return true
}

func (c *ConnectionManager) TargetConfigChan() chan<- *TargetConnectionControl {
	return c.targetsConfigChan
}

// Receives TargetConfiguration from the TargetConfigChan and connects/reconnects to local targets.
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
					} else if strings.HasPrefix(name, "*:") {
						c.config.Log.Info().Msgf("Initializing wildcard target %s.", name)
						newTargets[name] = &TargetState{
							config:      c.config,
							connManager: c,
							name:        name,
							queryTarget: "*",
							target:      newConfig,
							request:     targetControlMsg.Insert.Request[newConfig.Request],
						}
						go newTargets[name].connect(c.connLimit)
					} else {
						lockPath := MakeTargetLockPath(c.config.ZookeeperPrefix, name)
						clusterMemberAddress := c.config.ServerAddress + ":" + strconv.Itoa(c.config.ServerPort)
						// no previous targetCache existed
						c.config.Log.Info().Msgf("Initializing target %s.", name)
						newTargets[name] = &TargetState{
							config:      c.config,
							connManager: c,
							lock:        locking.NewZookeeperNonBlockingLock(c.zkConn, lockPath, clusterMemberAddress, zk.WorldACL(zk.PermAll)),
							name:        name,
							queryTarget: name,
							targetCache: c.cache.Add(name),
							target:      newConfig,
							request:     targetControlMsg.Insert.Request[newConfig.Request],
						}
						if c.zkConn != nil {
							go newTargets[name].connectWithLock(c.connLimit)
						} else {
							go newTargets[name].connect(c.connLimit)
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
