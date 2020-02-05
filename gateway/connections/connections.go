// Copyright 2020 Netflix Inc
// Author: Colin McIntosh

package connections

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/client"
	gnmiclient "github.com/openconfig/gnmi/client/gnmi"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	targetpb "github.com/openconfig/gnmi/proto/target"
	targetlib "github.com/openconfig/gnmi/target"
	"github.com/rs/zerolog/log"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway"
)

func NewConnectionManagerDefault(config *gateway.GatewayConfig) (*ConnectionManager, error) {
	mgr := ConnectionManager{config: config}

	if err := targetlib.Validate(mgr.config.TargetConfigurations); err != nil {
		return nil, fmt.Errorf("configuration is invalid: %v", err)
	}

	// Setup the cache
	cache.Type = cache.GnmiNoti
	mgr.cache = cache.New(nil)

	// TODO: Remove or enable these. Not sure what they do currently.
	// Start functions to periodically update metadata stored in the cache for each target.
	//go periodic(*metadataUpdatePeriod, mgr.cache.UpdateMetadata)
	//go periodic(*sizeUpdatePeriod, mgr.cache.UpdateSize)
	return &mgr, nil
}

type ConnectionManager struct {
	cache  *cache.Cache
	config *gateway.GatewayConfig
}

func (c *ConnectionManager) Cache() *cache.Cache {
	return c.cache
}

func (c *ConnectionManager) Start() {
	ctx := context.Background()
	for name, target := range c.config.TargetConfigurations.Target {
		c.config.Log.Info().Msgf("Initializing connection for %s.", name)
		go func(name string, target *targetpb.Target) {
			targetState := &TargetState{name: name, target: c.cache.Add(name)}

			request := c.config.TargetConfigurations.Request[target.Request]
			query, err := client.NewQuery(request)
			if err != nil {
				log.Error().Msgf("NewQuery(%s): %v", request.String(), err)
				return
			}
			query.Addrs = target.Addresses

			if target.Credentials != nil {
				query.Credentials = &client.Credentials{
					Username: target.Credentials.Username,
					Password: target.Credentials.Password,
				}
			}

			// TLS is always enabled for a target.
			query.TLS = &tls.Config{
				// Today, we assume that we should not verify the certificate from the target.
				InsecureSkipVerify: true,
			}

			query.Target = name
			query.Timeout = c.config.TargetDialTimeout

			query.ProtoHandler = targetState.handleUpdate

			if err := query.Validate(); err != nil {
				log.Error().Err(err).Msgf("query.Validate(): %v", err)
				return
			}
			cl := client.Reconnect(&client.BaseClient{}, targetState.disconnect, nil)
			if err := cl.Subscribe(ctx, query, gnmiclient.Type); err != nil {
				log.Error().Err(err).Msgf("Subscribe failed for target %q: %v", name, err)
			}
		}(name, target)
	}
}

// Container for some of the target TargetState data. It is created once
// for every device and used as a closure parameter by ProtoHandler.
type TargetState struct {
	name   string
	target *cache.Target
	// connected status is set to true when the first gnmi notification is received.
	// it gets reset to false when disconnect call back of ReconnectClient is called.
	connected bool
}

func (s *TargetState) disconnect() {
	s.connected = false
	s.target.Reset()
}

// handleUpdate parses a protobuf message received from the target. This implementation handles only
// gNMI SubscribeResponse messages. When the message is an Update, the GnmiUpdate method of the
// cache.Target is called to generate an update. If the message is a sync_response, then target is
// marked as synchronised.
func (s *TargetState) handleUpdate(msg proto.Message) error {
	if !s.connected {
		s.target.Connect()
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
		if err := s.target.GnmiUpdate(v.Update); err != nil {
			return fmt.Errorf("target cache update error: %s", err)
		}
	case *gnmipb.SubscribeResponse_SyncResponse:
		s.target.Sync()
	case *gnmipb.SubscribeResponse_Error:
		return fmt.Errorf("error in response: %s", v)
	default:
		return fmt.Errorf("unknown response %T: %s", v, v)
	}
	return nil
}
