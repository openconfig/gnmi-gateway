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

// Copyright 2018 Google Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Portions of this file including StartServer (excluding modifications) are from
// https://github.com/openconfig/gnmi/blob/89b2bf29312cda887da916d0f3a32c1624b7935f/cmd/gnmi_collector/gnmi_collector.go

package gateway

import (
	"context"
	"fmt"
	"github.com/openconfig/gnmi/cache"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/subscribe"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
)

// StartServer will start the gNMI server that serves the Subscribe interface to downstream gNMI clients.
func (g *Gateway) StartServer(c *cache.Cache) error {
	if g.config.ServerTLSCreds == nil {
		if g.config.ServerTLSCert == "" || g.config.ServerTLSKey == "" {
			return fmt.Errorf("no TLS creds; you must specify a TLS cert and key")
		}

		// Initialize TLS credentials.
		creds, err := credentials.NewServerTLSFromFile(g.config.ServerTLSCert, g.config.ServerTLSKey)
		if err != nil {
			return fmt.Errorf("failed to generate credentials: %v", err)
		}
		g.config.ServerTLSCreds = creds
	}

	// Create a grpc Server.
	srv := grpc.NewServer(grpc.Creds(g.config.ServerTLSCreds))
	// Initialize gNMI Proxy Subscribe server.
	subscribeSrv, err := subscribe.NewServer(c)
	if err != nil {
		return fmt.Errorf("Could not instantiate gNMI server: %v", err)
	}
	gnmipb.RegisterGNMIServer(srv, subscribeSrv)
	// Forward streaming updates to clients.
	g.AddClient(subscribeSrv.Update)
	// Register listening port and start serving.
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", g.config.ServerPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	go func() {
		err := srv.Serve(lis) // blocks
		g.config.Log.Error().Err(err).Msg("Error running gNMI server.")
	}()
	defer srv.Stop()
	ctx := context.Background()
	<-ctx.Done()
	return ctx.Err()
}
