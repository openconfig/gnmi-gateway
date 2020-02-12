// Copyright 2020 Netflix Inc
// Author: Colin McIntosh (colin@netflix.com)

package gateway

import (
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
	TargetConfigurationJSONFile string
	// Interval to reload the target configuration JSON file.
	TargetConfigurationJSONFileReloadInterval time.Duration
	// Timeout for dialing the target connection
	TargetDialTimeout time.Duration
}

func NewDefaultGatewayConfig() *GatewayConfig {
	config := &GatewayConfig{
		Log: log.Logger,
	}
	return config
}
