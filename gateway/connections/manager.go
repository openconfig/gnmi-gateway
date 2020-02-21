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
	"strings"
	"sync"
)

func NewConnectionManagerDefault(config *configuration.GatewayConfig) (*ConnectionManager, error) {
	mgr := ConnectionManager{
		config:            config,
		connLimit:         semaphore.NewWeighted(int64(config.TargetLimit)),
		targets:           make(map[string]*TargetState),
		targetsConfigChan: make(chan *targetpb.Configuration, 1),
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

func (c *ConnectionManager) TargetConfigChan() chan *targetpb.Configuration {
	return c.targetsConfigChan
}

// Receives TargetConfiguration from the TargetConfigChan and connects/reconnects to targets.
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
				} else {
					// no previous targetCache existed
					c.config.Log.Info().Msgf("Initializing target %s.", name)
					newTargets[name] = &TargetState{
						config:      c.config,
						lock:        locking.NewZookeeperNonBlockingLock(c.zkConn, "/gnmi/target/"+name, zk.WorldACL(zk.PermAll)),
						name:        name,
						targetCache: c.cache.Add(name),
						target:      target,
						request:     targetConfig.Request[target.Request],
					}
					go newTargets[name].connectWithLock(c.connLimit)
				}
			}

			// save the target changes
			c.targets = newTargets
			c.targetsMutex.Unlock()
		}
	}
}

func (c *ConnectionManager) Start() error {
	c.config.Log.Info().Msgf("Connecting to Zookeeper hosts: %v.", strings.Join(c.config.ZookeeperHosts, ", "))
	newConn, eventChan, err := zk.Connect(c.config.ZookeeperHosts, c.config.ZookeeperTimeout)
	if err != nil {
		c.config.Log.Error().Err(err).Msgf("Unable to connect to Zookeeper: %v", err)
		return err
	}
	c.zkConn = newConn
	go c.zookeeperEventHandler(eventChan)
	c.config.Log.Info().Msg("Zookeeper connected.")
	go c.ReloadTargets()
	return nil
}

func (c *ConnectionManager) zookeeperEventHandler(zkEventChan <-chan zk.Event) {
	for {
		select {
		case event := <-zkEventChan:
			if event.State != zk.StateUnknown {
				switch event.State {
				case zk.StateConnected:
					log.Info().Msg("Connected to Zookeeper.")
				case zk.StateConnecting:
					log.Info().Msg("Attempting to connect to Zookeeper.")
					//c.LockAcquired = false
				case zk.StateDisconnected:
					log.Info().Msg("Zookeeper disconnected.")
					//c.LockAcquired = false
				case zk.StateHasSession:
					log.Info().Msg("Zookeeper session established.")
				default:
					log.Info().Msgf("Got Zookeeper state update: %v", event.State.String())
				}
			}
		}
	}
}
