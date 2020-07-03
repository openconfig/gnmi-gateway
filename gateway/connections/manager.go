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
		targetsConfigChan: make(chan *targetpb.Configuration, 1),
		zkConn:            zkConn,
	}
	cache.Type = cache.GnmiNoti
	mgr.cache = cache.New(nil)
	return &mgr, nil
}

type ConnectionManager struct {
	cache             *cache.Cache
	config            *configuration.GatewayConfig
	connLimit         *semaphore.Weighted
	targets           map[string]*TargetState
	targetsMutex      sync.Mutex
	targetsConfigChan chan *targetpb.Configuration
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

func (c *ConnectionManager) TargetConfigChan() chan *targetpb.Configuration {
	return c.targetsConfigChan
}

// Receives TargetConfiguration from the TargetConfigChan and connects/reconnects to local targets.
func (c *ConnectionManager) ReloadTargets() {
	for {
		select {
		case targetConfig := <-c.targetsConfigChan:
			log.Info().Msg("Connection manager received a Target Configuration")
			if err := targetlib.Validate(targetConfig); err != nil {
				c.config.Log.Error().Err(err).Msgf("configuration is invalid: %v", err)
			}

			c.targetsMutex.Lock()
			newTargets := make(map[string]*TargetState)
			// disconnect from everything we don't have a target config for
			for name, target := range c.targets {
				if _, exists := targetConfig.Target[name]; !exists {
					err := target.disconnect()
					if err != nil {
						c.config.Log.Warn().Err(err).Msgf("error while disconnecting from target %s: %v", name, err)
					}
				}
			}

			// make new connection or reconnect as needed
			for name, target := range targetConfig.Target {
				if currentTargetState, exists := c.targets[name]; exists {
					// previous target exists

					if !currentTargetState.Equal(target) {
						// target is different
						c.config.Log.Info().Msgf("Updating connection for %s.", name)
						currentTargetState.target = target
						currentTargetState.request = targetConfig.Request[target.Request]

						if currentTargetState.connecting || currentTargetState.connected {
							err := newTargets[name].reconnect()
							if err != nil {
								c.config.Log.Error().Err(err).Msgf("Error reconnecting connection for %s", name)
							}
						}
					}
					newTargets[name] = currentTargetState
				} else if strings.HasPrefix(name, "*:") {
					c.config.Log.Info().Msgf("Initializing wildcard target %s.", name)
					newTargets[name] = &TargetState{
						config:      c.config,
						connManager: c,
						name:        name,
						queryTarget: "*",
						target:      target,
						request:     targetConfig.Request[target.Request],
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
						target:      target,
						request:     targetConfig.Request[target.Request],
					}
					if c.zkConn != nil {
						go newTargets[name].connectWithLock(c.connLimit)
					} else {
						go newTargets[name].connect(c.connLimit)
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
