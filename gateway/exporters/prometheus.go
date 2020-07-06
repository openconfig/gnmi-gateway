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

package exporters

import (
	"errors"
	"fmt"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/openconfig"
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"strings"
)

func NewPrometheusExporter(config *configuration.GatewayConfig) Exporter {
	return &PrometheusExporter{
		config:     config,
		deltaCalc:  NewDeltaCalculator(),
		metrics:    make(map[Hash]prometheus.Metric),
		typeLookup: new(openconfig.TypeLookup),
	}
}

type PrometheusExporter struct {
	config     *configuration.GatewayConfig
	cache      *cache.Cache
	deltaCalc  *DeltaCalculator
	metrics    map[Hash]prometheus.Metric
	typeLookup *openconfig.TypeLookup
}

func (e *PrometheusExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)
	for _, update := range notification.Update {
		value, isNumber := GetNumberValues(update.Val)
		if !isNumber {
			continue
		}
		metricName, labels := UpdateToMetricNameAndLabels(update)
		metricHash := NewStringMapHash(metricName, labels)

		metric, exists := e.metrics[metricHash]
		if !exists {
			var path []string
			for _, elem := range update.Path.Elem {
				path = append(path, elem.Name)
			}
			metricType := e.typeLookup.GetTypeByPath(path)

			switch metricType {
			case "counter64":
				metric = promauto.NewCounter(prometheus.CounterOpts{
					Name:        metricName,
					ConstLabels: labels,
				})
			case "gauge32":
			default:
				metric = promauto.NewGauge(prometheus.GaugeOpts{
					Name:        metricName,
					ConstLabels: labels,
				})
			}
			e.metrics[metricHash] = metric
		}

		switch m := metric.(type) {
		case prometheus.Counter:
			delta, _ := e.deltaCalc.Calc(metricHash, value)
			m.Add(delta)
		case prometheus.Gauge:
			m.Set(value)
		}
	}
}

func (e *PrometheusExporter) Start(cache *cache.Cache) error {
	e.config.Log.Info().Msg("Starting Prometheus exporter.")
	if e.config.OpenConfigDirectory == "" {
		return errors.New("value is not set for OpenConfigDirectory configuration")
	}
	e.cache = cache
	err := e.typeLookup.LoadAllModules(e.config.OpenConfigDirectory)
	if err != nil {
		e.config.Log.Error().Err(err).Msgf("Unable to load OpenConfig modules in %s: %v", e.config.OpenConfigDirectory, err)
		return err
	}

	e.config.Log.Info().Msg("Starting Prometheus exporter.")
	e.cache.SetClient(e.Export)
	go e.runHttpServer()
	return nil
}

func (e *PrometheusExporter) runHttpServer() {
	var errCount = 0
	var lastError error
	for {
		e.config.Log.Info().Msg("Starting Prometheus HTTP server.")
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(":59100", nil)
		if err != nil {
			e.config.Log.Error().Err(err).Msgf("Prometheus HTTP server stopped with an error: %v", err)
			if err.Error() == lastError.Error() {
				errCount = errCount + 1
				if errCount >= 3 {
					panic(fmt.Errorf("too many errors returned by Prometheus HTTP server: %s", err.Error()))
				}
			} else {
				errCount = 0
				lastError = err
			}
		}
	}
}

func UpdateToMetricNameAndLabels(update *gnmipb.Update) (string, map[string]string) {
	metricName := ""
	labels := make(map[string]string)
	for _, elem := range update.Path.Elem {
		elemName := strings.ReplaceAll(elem.Name, "-", "_")
		if metricName == "" {
			metricName = elemName
		} else {
			metricName = metricName + "_" + elemName
		}

		for key, value := range elem.Key {
			labelKey := metricName + "_" + strings.ReplaceAll(key, "-", "_")
			labels[labelKey] = value
		}
	}
	return metricName, labels
}
