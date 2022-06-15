package statsd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cactus/go-statsd-client/v5/statsd"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

const Name = "statsd"

var _ exporters.Exporter = new(StatsdExporter)

type Metric struct {
	Account     string            `json:"Account"`
	Measurement string            `json:"Metric"`
	Namespace   string            `json:"Namespace"`
	Dims        map[string]string `json:"Dims"`
	Value       interface{}       `json:"-"`
}

type Point struct {
	Tags   map[string]string
	Fields map[string]interface{}
}

func init() {
	exporters.Register(Name, NewStatsdExporter)
}

func NewStatsdExporter(config *configuration.GatewayConfig) exporters.Exporter {
	return &StatsdExporter{
		config: config,
	}
}

type StatsdExporter struct {
	config  *configuration.GatewayConfig
	connMgr *connections.ConnectionManager
	client  statsd.Statter
}

func (e *StatsdExporter) Name() string {
	return Name
}

func (e *StatsdExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)

	for _, update := range notification.Update {
		point := Point{
			Fields: make(map[string]interface{}),
		}

		metric := Metric{}

		timestamp := time.Unix(0, notification.Timestamp)
		beforeLimit := (time.Now()).Add(-30 * time.Minute)
		afterLimit := (time.Now()).Add(4 * time.Minute)

		if timestamp.Before(beforeLimit) || timestamp.After(afterLimit) {
			return
		}

		var name string

		value, valid := utils.GetValues(update.Val)

		if !valid {
			continue
		}

		metric.Value = value

		elems, keys, err := extractPrefixAndPathKeys(notification.GetPrefix(), update.GetPath())
		if err != nil {
			e.config.Log.Info().Msg(fmt.Sprintf("Failed to extract path or keys: %s", err))
		}

		name, elems = elems[len(elems)-1], elems[:len(elems)-1]

		if metric.Measurement == "" {
			measurementPath := strings.Join(elems, "/")

			p := notification.GetPrefix()
			target := p.GetTarget()
			origin := p.GetOrigin()

			keys["path"] = measurementPath
			if origin != "" {
				keys["origin"] = origin
			}

			if target != "" {
				keys["target"] = target
			}

			metric.Measurement = pathToMetricName(measurementPath)

			point.Tags = keys

			targetConfig, found := (*e.connMgr).GetTargetConfig(point.Tags["target"])

			if found {
				for _, fieldName := range e.config.ExporterMetadataAllowlist {
					fieldVal, exists := targetConfig.Meta[fieldName]
					if exists {
						point.Tags[fieldName] = fieldVal
					} else {
						e.config.Log.Error().Msg("Field " + fieldName + " is not set for device " + point.Tags["target"] + " but is listed in the exporter's Metadata allow-list")
						return
					}
				}

			} else {
				e.config.Log.Error().Msg("Target config not found for target: " + point.Tags["target"])
				return
			}
		}

		point.Fields[pathToMetricName(name)] = value

		if metric.Measurement == "" {
			e.config.Log.Info().Msg("Point measurement is empty. Returning.")
			return
		}

		metric.Dims = point.Tags
		metric.Account = metric.Dims["Account"]
		delete(metric.Dims, "Account")

		metric.Namespace = "Interface metrics"

		metricJSON, err := json.Marshal(metric)

		if err != nil {
			e.config.Log.Error().Msg("Failed to marshal point into JSON")
			return
		}

		// e.config.Log.Info().Msg(string(metricJSON))

		if err != nil {
			e.config.Log.Error().Msg(err.Error())
			return
		}

		switch metric.Value.(type) {
		case int64:
			if err := e.client.Gauge(string(metricJSON), int64(metric.Value.(int64)), 1); err != nil {
				e.config.Log.Error().Msg(err.Error())
			}
		case int32:
			if err := e.client.Gauge(string(metricJSON), int64(metric.Value.(int32)), 1); err != nil {
				e.config.Log.Error().Msg(err.Error())
			}
		case float64:
			if err := e.client.Gauge(string(metricJSON), int64(metric.Value.(float64)), 1); err != nil {
				e.config.Log.Error().Msg(err.Error())
			}
		case float32:
			if err := e.client.Gauge(string(metricJSON), int64(metric.Value.(float32)), 1); err != nil {
				e.config.Log.Error().Msg(err.Error())
			}
		case string:
			if err := e.client.Set(string(metricJSON), string(metric.Value.(string)), 1); err != nil {
				e.config.Log.Error().Msg(err.Error())
			}
		}
	}
}

func (e *StatsdExporter) Start(connMgr *connections.ConnectionManager) error {

	e.config.Log.Info().Msg("Starting Statsd exporter.")

	e.connMgr = connMgr

	var err error
	e.client, err = statsd.NewClient(e.config.Exporters.StatsdHost, "")

	if err != nil {
		return err
	}

	return nil
}

func pathToMetricName(metricPath string) string {
	return strings.ReplaceAll(strings.ReplaceAll(metricPath, "/", "_"), "-", "_")
}

func extractPrefixAndPathKeys(prefix *gnmipb.Path, metricPath *gnmipb.Path) ([]string, map[string]string, error) {
	elems := make([]string, 0)
	keys := make(map[string]string)
	for _, metricPath := range []*gnmipb.Path{prefix, metricPath} {
		for _, e := range metricPath.Elem {
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
