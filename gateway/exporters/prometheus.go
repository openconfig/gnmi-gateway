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
	"github.com/cespare/xxhash/v2"
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"sort"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/openconfig"
	"strings"
	"sync"
)

func NewPrometheusExporter(config *configuration.GatewayConfig) Exporter {
	return &PrometheusExporter{
		config:     config,
		deltaCalc:  NewDeltaCalculator(),
		metrics:    make(map[MetricHash]prometheus.Metric),
		typeLookup: new(openconfig.TypeLookup),
	}
}

type PrometheusExporter struct {
	config     *configuration.GatewayConfig
	cache      *cache.Cache
	deltaCalc  *DeltaCalculator
	metrics    map[MetricHash]prometheus.Metric
	typeLookup *openconfig.TypeLookup
}

type MetricHash uint64

func (e *PrometheusExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)
	for _, update := range notification.Update {
		value, isNumber := GetNumberValues(update.Val)
		if !isNumber {
			continue
		}
		metricName, labels := UpdateToMetricNameAndLabels(update)
		metricHash := NewMetricHash(metricName, labels)

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
	if e.config.OpenConfigModelDirectory == "" {
		return errors.New("value is not set for OpenConfigModelDirectory configuration")
	}
	e.cache = cache
	err := e.typeLookup.LoadAllModules(e.config.OpenConfigModelDirectory)
	if err != nil {
		e.config.Log.Error().Err(err).Msgf("Unable to load OpenConfig modules in %s: %v", e.config.OpenConfigModelDirectory, err)
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

func NewDeltaCalculator() *DeltaCalculator {
	return &DeltaCalculator{
		history: make(map[MetricHash]float64),
	}
}

type DeltaCalculator struct {
	lock    sync.Mutex
	history map[MetricHash]float64
}

// Calculate the delta for given hash and value. Returns the provided value and false if a previous value didn't exist.
func (d *DeltaCalculator) Calc(hash MetricHash, newValue float64) (float64, bool) {
	d.lock.Lock()
	defer d.lock.Unlock()
	oldValue, exists := d.history[hash]
	d.history[hash] = newValue
	return newValue - oldValue, exists
}

func GetNumberValues(tv *gnmipb.TypedValue) (float64, bool) {
	if tv != nil && tv.Value != nil {
		switch tv.Value.(type) {
		case *gnmipb.TypedValue_StringVal:
			return 0, false
		case *gnmipb.TypedValue_IntVal:
			return float64(tv.GetIntVal()), true
		case *gnmipb.TypedValue_UintVal:
			return float64(tv.GetUintVal()), true
		case *gnmipb.TypedValue_BoolVal:
			if tv.GetBoolVal() {
				return 1, true
			} else {
				return 0, false
			}
		case *gnmipb.TypedValue_FloatVal:
			return float64(tv.GetFloatVal()), true
		case *gnmipb.TypedValue_LeaflistVal:
			return 0, false
		case *gnmipb.TypedValue_BytesVal:
			return 0, false
		default:
			return 0, false
		}
	}
	return 0, false
}

func UpdateToMetricNameAndLabels(update *gnmipb.Update) (string, prometheus.Labels) {
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

func NewMetricHash(name string, labels prometheus.Labels) MetricHash {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var hash string
	for _, k := range keys {
		hash += k
		hash += labels[k]
	}
	return MetricHash(xxhash.Sum64String(name + hash))
}
