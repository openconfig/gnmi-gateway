// Copyright 2020 Netflix Inc
// Author: Colin McIntosh

package gateway

import (
	targetpb "github.com/openconfig/gnmi/proto/target"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"time"
)

type GatewayConfig struct {
	// Logger used by the Gateway code
	Log zerolog.Logger

	// Port to server the gNMI server on
	ServerPort int
	// gNMI Server TLS Cert
	ServerTLSCert string
	// gNMI Server TLS Key
	ServerTLSKey string

	// Configs for connections to targets
	TargetConfigurations *targetpb.Configuration
	// Timeout for dialing the target connection
	TargetDialTimeout time.Duration
}

func NewDefaultGatewayConfig() *GatewayConfig {
	config := &GatewayConfig{
		Log: log.Logger,
	}
	return config
}
