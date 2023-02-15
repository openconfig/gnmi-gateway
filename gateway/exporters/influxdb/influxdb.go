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

package influxdb

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/mspiez/gnmi-gateway/gateway/configuration"
	"github.com/mspiez/gnmi-gateway/gateway/exporters"
	"github.com/mspiez/gnmi-gateway/gateway/utils"
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

const Name = "influxdb"

var _ exporters.Exporter = new(InfluxDBExporter)

type Point struct {
	Tags        map[string]string
	Measurement string
	Fields      map[string]interface{}
	Timestamp   time.Time
}

func init() {
	exporters.Register(Name, NewInfluxDBExporter)
}

func NewInfluxDBExporter(config *configuration.GatewayConfig) exporters.Exporter {

	return &InfluxDBExporter{
		config: config,
	}
}

type InfluxDBExporter struct {
	cache  *cache.Cache
	config *configuration.GatewayConfig
	client influxdb2.Client
}

func (e *InfluxDBExporter) Name() string {
	return Name
}

func (e *InfluxDBExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)

	point := Point{
		Timestamp: time.Unix(0, notification.Timestamp),
		Tags:      make(map[string]string),
		Fields:    make(map[string]interface{}),
	}

	for _, update := range notification.Update {
		var name string

		value, isNumber := utils.GetNumberValues(update.Val)

		if !isNumber {
			continue
		}

		elems, keys, err := extractPrefixAndPathKeys(notification.GetPrefix(), update.GetPath())
		if err != nil {
			e.config.Log.Info().Msg(fmt.Sprintf("Failed to extract path or keys: %s", err))
		}

		name, elems = elems[len(elems)-1], elems[:len(elems)-1]

		if point.Measurement == "" {
			path := strings.Join(elems, "/")

			p := notification.GetPrefix()
			target := p.GetTarget()
			origin := p.GetOrigin()

			keys["path"] = path
			if origin != "" {
				keys["origin"] = origin
			}

			if target != "" {
				keys["target"] = target
			}

			point.Measurement = pathToMetricName(path)
			point.Tags = keys
		}

		point.Fields[pathToMetricName(name)] = value
	}

	if point.Measurement == "" {
		return
	}

	writer := e.client.WriteAPI(
		e.config.Exporters.InfluxDBOrg,
		e.config.Exporters.InfluxDBBucket)

	errorsCh := writer.Errors()
	go func() {
		for err := range errorsCh {
			e.config.Log.Info().Msg(fmt.Sprintf("write error: %s\n", err.Error()))
		}
	}()

	p := influxdb2.NewPoint(
		point.Measurement,
		point.Tags,
		point.Fields,
		point.Timestamp)

	writer.WritePoint(p)
}

func (e *InfluxDBExporter) Start(cache *cache.Cache) error {
	_ = cache
	e.config.Log.Info().Msg("Starting InfluxDBv2 exporter.")

	_, err := url.ParseRequestURI(e.config.Exporters.InfluxDBTarget)
	if err != nil {
		return errors.New(fmt.Sprintf(
			"InfluxDB target URL '%s' is invalid", e.config.Exporters.InfluxDBTarget))
	}

	if e.config.Exporters.InfluxDBToken == "" {
		return errors.New("InfluxDB token is not set")
	}

	if e.config.Exporters.InfluxDBOrg == "" {
		return errors.New("InfluxDB organization is not set")
	}

	if e.config.Exporters.InfluxDBBucket == "" {
		return errors.New("InfluxDB bucket is not set")
	}

	e.client = influxdb2.NewClientWithOptions(
		e.config.Exporters.InfluxDBTarget,
		e.config.Exporters.InfluxDBToken,
		influxdb2.DefaultOptions().SetBatchSize(e.config.Exporters.InfluxDBBatchSize))

	return nil
}

func pathToMetricName(path string) string {
	return strings.ReplaceAll(strings.ReplaceAll(path, "/", "_"), "-", "_")
}

func extractPrefixAndPathKeys(prefix *gnmipb.Path, path *gnmipb.Path) ([]string, map[string]string, error) {
	elems := make([]string, 0)
	keys := make(map[string]string)
	for _, path := range []*gnmipb.Path{prefix, path} {
		for _, e := range path.Elem {
			name := e.Name
			elems = append(elems, name)

			for k, v := range e.GetKey() {
				if _, ok := keys[k]; ok {
					keys[name+"/"+k] = v
				} else {
					keys[k] = v
				}
			}
		}
	}

	if len(elems) == 0 {
		return elems, keys, errors.New("Path contains no elems")
	}

	return elems, keys, nil
}
