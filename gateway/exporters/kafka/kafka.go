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

package kafka

import (
	"context"
	"errors"

	"github.com/golang/protobuf/proto"
	"github.com/openconfig/gnmi-gateway/gateway/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
)

const Name = "kafka"

var _ exporters.Exporter = new(KafkaExporter)

func init() {
	exporters.Register(Name, NewKafkaExporter)
}

func NewKafkaExporter(config *configuration.GatewayConfig) exporters.Exporter {
	return &KafkaExporter{
		config: config,
	}
}

type KafkaExporter struct {
	config *configuration.GatewayConfig
	cache  *cache.Cache
	writer *kafka.Writer
}

func (e *KafkaExporter) Name() string {
	return Name
}

func (e *KafkaExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)

	data, err := proto.Marshal(notification)
	if err != nil {
		e.config.Log.Warn().Msgf("failed to marshal message for Kafka: %s", err)
		return
	}

	err = e.writer.WriteMessages(context.Background(),
		kafka.Message{
			Key:   []byte(utils.PathToXPath(notification.Prefix)),
			Value: data,
		},
	)
	if err != nil {
		e.config.Log.Warn().Msgf("failed to write message to Kafka: %s", err)
	}
}

func (e *KafkaExporter) Start(connMgr *connections.ConnectionManager) error {
	e.config.Log.Info().Msg("Starting Kafka exporter.")

	if e.config.Exporters.KafkaTopic == "" {
		return errors.New("configuration option for Kafka Topic is not set")
	}

	if len(e.config.Exporters.KafkaBrokers) < 0 {
		return errors.New("configuration option for Kafka Brokers is not set")
	}

	e.writer = &kafka.Writer{
		Addr:         kafka.TCP(e.config.Exporters.KafkaBrokers...),
		Topic:        e.config.Exporters.KafkaTopic,
		Balancer:     &kafka.LeastBytes{},
		BatchBytes:   e.config.Exporters.KafkaBatchBytes,
		BatchSize:    e.config.Exporters.KafkaBatchSize,
		BatchTimeout: e.config.Exporters.KafkaBatchTimeout,
		Async:        true,
		ErrorLogger: kafkaLogger{
			config: e.config,
			level:  zerolog.ErrorLevel,
		},
	}

	if e.config.Exporters.KafkaLogging {
		e.writer.Logger = kafkaLogger{
			config: e.config,
			level:  zerolog.InfoLevel,
		}
	}

	return nil
}

type kafkaLogger struct {
	config *configuration.GatewayConfig
	level  zerolog.Level
}

func (k kafkaLogger) Printf(s string, i ...interface{}) {
	k.config.Log.WithLevel(k.level).Msgf("Kafka: "+s, i...)
}
