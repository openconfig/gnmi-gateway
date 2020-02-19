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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/client"
	gnmiclient "github.com/openconfig/gnmi/client/gnmi"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	targetpb "github.com/openconfig/gnmi/proto/target"
	targetlib "github.com/openconfig/gnmi/target"
	"github.com/rs/zerolog/log"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"sync"
)

func NewConnectionManagerDefault(config *configuration.GatewayConfig) (*ConnectionManager, error) {
	mgr := ConnectionManager{
		config:            config,
		targets:           make(map[string]*TargetState),
		targetsConfigChan: make(chan *targetpb.Configuration, 1),
	}

	// Setup the cache
	cache.Type = cache.GnmiNoti
	mgr.cache = cache.New(nil)

	// TODO: Remove or enable these. Not sure what they do currently.
	// Start functions to periodically update metadata stored in the cache for each targetCache.
	//go periodic(*metadataUpdatePeriod, mgr.cache.UpdateMetadata)
	//go periodic(*sizeUpdatePeriod, mgr.cache.UpdateSize)
	return &mgr, nil
}

type ConnectionManager struct {
	cache             *cache.Cache
	config            *configuration.GatewayConfig
	targets           map[string]*TargetState
	targetsMutex      sync.Mutex
	targetsConfigChan chan *targetpb.Configuration
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

			newTargets := make(map[string]*TargetState)
			for name, target := range targetConfig.Target {
				if currentTargetState, exists := c.targets[name]; exists {
					// previous target exists
					newTargets[name] = currentTargetState
					if targetConfigChanged(target, newTargets[name]) {
						// target is different
						c.config.Log.Info().Msgf("Updating connection for %s.", name)
						err := newTargets[name].client.Close() // this will disconnect and reset the cache via the disconnect callback
						if err != nil {
							c.config.Log.Error().Err(err).Msgf("Error closing connection for %s", name)
						}
						newTargets[name].target = target
						newTargets[name].request = targetConfig.Request[target.Request]
						go c.connectToTarget(newTargets[name])
					}
				} else {
					// no previous targetCache existed
					c.config.Log.Info().Msgf("Initializing connection for %s.", name)
					newTargets[name] = &TargetState{
						config:      c.config,
						name:        name,
						targetCache: c.cache.Add(name),
						ctx:         context.Background(),
						target:      target,
						request:     targetConfig.Request[target.Request],
					}
					go c.connectToTarget(newTargets[name])
				}
			}
			c.targetsMutex.Lock()
			c.targets = newTargets
			c.targetsMutex.Unlock()
		}
	}
}

func (c *ConnectionManager) connectToTarget(targetState *TargetState) {
	query, err := client.NewQuery(targetState.request)
	if err != nil {
		log.Error().Msgf("NewQuery(%s): %v", targetState.request.String(), err)
		return
	}
	query.Addrs = targetState.target.Addresses

	if targetState.target.Credentials != nil {
		query.Credentials = &client.Credentials{
			Username: targetState.target.Credentials.Username,
			Password: targetState.target.Credentials.Password,
		}
	}

	// TLS is always enabled for a targetCache.
	query.TLS = &tls.Config{
		// Today, we assume that we should not verify the certificate from the targetCache.
		InsecureSkipVerify: true,
	}

	query.Target = targetState.name
	query.Timeout = c.config.TargetDialTimeout

	query.ProtoHandler = targetState.handleUpdate

	if err := query.Validate(); err != nil {
		log.Error().Err(err).Msgf("query.Validate(): %v", err)
		return
	}
	targetState.client = client.Reconnect(&client.BaseClient{}, targetState.disconnect, nil)
	if err := targetState.client.Subscribe(targetState.ctx, query, gnmiclient.Type); err != nil {
		log.Error().Err(err).Msgf("Subscribe failed for targetCache %q: %v", targetState.name, err)
	}
}

func (c *ConnectionManager) Start() {
	go c.ReloadTargets()
}

func targetConfigChanged(target *targetpb.Target, currentState *TargetState) bool {
	if len(currentState.target.Addresses) != len(target.Addresses) {
		return true
	}
	for i, addr := range currentState.target.Addresses {
		if target.Addresses[i] != addr {
			return true
		}
	}
	if currentState.target.Credentials.Username != target.Credentials.Username {
		return true
	}
	if currentState.target.Credentials.Password != target.Credentials.Password {
		return true
	}
	return false
}

// Container for some of the targetCache TargetState data. It is created once
// for every device and used as a closure parameter by ProtoHandler.
type TargetState struct {
	config      *configuration.GatewayConfig
	name        string
	targetCache *cache.Target
	// connected status is set to true when the first gnmi notification is received.
	// it gets reset to false when disconnect call back of ReconnectClient is called.
	connected bool
	ctx       context.Context
	client    *client.ReconnectClient
	target    *targetpb.Target
	request   *gnmipb.SubscribeRequest
}

func (s *TargetState) disconnect() {
	s.connected = false
	s.targetCache.Disconnect()
	s.targetCache.Reset()
}

// handleUpdate parses a protobuf message received from the targetCache. This implementation handles only
// gNMI SubscribeResponse messages. When the message is an Update, the GnmiUpdate method of the
// cache.Target is called to generate an update. If the message is a sync_response, then targetCache is
// marked as synchronised.
func (s *TargetState) handleUpdate(msg proto.Message) error {
	//fmt.Printf("%+v\n", msg)
	if !s.connected {
		s.targetCache.Connect()
		s.connected = true
	}
	resp, ok := msg.(*gnmipb.SubscribeResponse)
	if !ok {
		return fmt.Errorf("failed to type assert message %#v", msg)
	}
	switch v := resp.Response.(type) {
	case *gnmipb.SubscribeResponse_Update:
		// Gracefully handle gNMI implementations that do not set Prefix.Target in their
		// SubscribeResponse Updates.
		if v.Update.GetPrefix() == nil {
			v.Update.Prefix = &gnmipb.Path{}
		}
		if v.Update.Prefix.Target == "" {
			v.Update.Prefix.Target = s.name
		}
		if err := s.rejectUpdate(v.Update); err != nil {
			//s.config.Log.Warn().Msgf("Update rejected: %s: %+v", err, v.Update)
			return nil
		}
		err := s.targetCache.GnmiUpdate(v.Update)
		if err != nil {
			return fmt.Errorf("targetCache cache update error: %s: %+v", err, v.Update)
		}
	case *gnmipb.SubscribeResponse_SyncResponse:
		s.config.Log.Debug().Msgf("Target is synced: %s", s.name)
		s.targetCache.Sync()
	case *gnmipb.SubscribeResponse_Error:
		return fmt.Errorf("error in response: %s", v)
	default:
		return fmt.Errorf("unknown response %T: %s", v, v)
	}
	return nil
}

func (s *TargetState) rejectUpdate(notification *gnmipb.Notification) error {
	for _, update := range notification.GetUpdate() {
		path := update.GetPath().GetElem()
		if len(path) >= 2 {
			if path[0].Name == "interfaces" && path[1].Name == "interface" {
				if value, exists := path[1].Key["name"]; exists {
					if value == "interface" {
						return errors.New("bug for Arista interface path") // Arista BUG #??????????
					}
				}
			}
		}
	}
	return nil
}
