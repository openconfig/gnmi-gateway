package statsd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/cactus/go-statsd-client/v5/statsd"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
	"github.com/openconfig/gnmi/ctree"
	"github.com/openconfig/gnmi/proto/gnmi"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

const Name = "statsd"

var _ exporters.Exporter = new(StatsdExporter)

type Metric struct {
	Account     string            `json:",omitempty"`
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
		//TODO Check if Geneva has timestamp limitations

		if timestamp.Before(beforeLimit) || timestamp.After(afterLimit) {
			return
		}

		var name string

		value, valid := utils.GetValues(update.Val)

		if !valid {
			continue
		}

		metric.Value = value

		path := utils.GetTrimmedPath(notification.Prefix, update.Path)
		metric.Namespace = e.config.GetPathMetadata(path)["Namespace"]
		if metric.Namespace == "" {
			metric.Namespace = "Default"
		}

		metric.Account = os.Getenv("MDM_ACCOUNT")

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

		if resourceId := os.Getenv("EXTENSION_ARMID"); resourceId != "" {
			metric.Dims["microsoft.resourceid"] = resourceId
		}

		metricJSON, err := json.Marshal(metric)

		if err != nil {
			e.config.Log.Error().Msg("Failed to marshal point into JSON")
			return
		}

		if err != nil {
			e.config.Log.Error().Msg(err.Error())
			return
		}

		val, isNumericValue := utils.GetNumberValues(update.Val)
		// Modify logic based on path meta regarding metric type (metric/log)
		if isNumericValue {
			e.config.Log.Debug().Msgf("%s:%d|g", string(metricJSON), int64(val))
			if err := e.client.Gauge(string(metricJSON), int64(val), 1); err != nil {
				e.config.Log.Error().Msg(err.Error())
			}
		} else if reflect.TypeOf(metric.Value) == reflect.TypeOf((*gnmi.TypedValue_StringVal)(nil)) {
			e.config.Log.Debug().Msgf("%s:%s|s", string(metricJSON), string(metric.Value.(*gnmi.TypedValue_StringVal).StringVal))

			// TODO Handle logging instead of pushing as metric
			// if err := e.client.Set(string(metricJSON), string(metric.Value.(*gnmi.TypedValue_StringVal).StringVal), 1); err != nil {
			// 	e.config.Log.Error().Msg(err.Error())
			// }
		} else {
			e.config.Log.Debug().Msgf("Received metric of type: %s", reflect.TypeOf(metric.Value).String())
		}
	}
}

func (e *StatsdExporter) Start(connMgr *connections.ConnectionManager) error {

	e.config.Log.Info().Msg("Starting Statsd exporter.")

	e.connMgr = connMgr

	var err error
	// e.client, err = statsd.NewClient(e.config.Exporters.StatsdHost, "")
	e.client, err = statsd.NewClientWithConfig(&statsd.ClientConfig{
		Address: e.config.Exporters.StatsdHost,
	})

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
				keys[e.Name+"_"+k] = v
			}
		}
	}

	if len(elems) == 0 {
		return elems, keys, errors.New("path contains no elems")
	}

	return elems, keys, nil
}
