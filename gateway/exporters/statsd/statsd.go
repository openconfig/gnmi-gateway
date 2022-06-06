package Statsd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"path"

	statsd "github.com/etsy/statsd/examples/go"
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
	Timestamp  time.Time `json:"time"`
	MetricData Data      `json:"data"`
}
type Data struct {
	Base BaseData `json:"baseData"`
}
type BaseData struct {
	Measurement string                   `json:"metric"`
	Namespace   string                   `json:"namespace"`
	DimNames    []string                 `json:"dimNames"`
	Series      []map[string]interface{} `json:"series"`
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
	client  *statsd.StatsdClient
}

func (e *StatsdExporter) Name() string {
	return Name
}

func (e *StatsdExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)

	point := Point{
		Fields: make(map[string]interface{}),
	}

	baseData := BaseData{}

	timestamp := time.Unix(0, notification.Timestamp)
	beforeLimit := (time.Now()).Add(-30 * time.Minute)
	afterLimit := (time.Now()).Add(4 * time.Minute)

	if timestamp.Before(beforeLimit) || timestamp.After(afterLimit) {
		return
	}

	metric := Metric{
		Timestamp: timestamp,
	}

	var deviceName string
	var rackName string
	var fabricName string

	// foundNumeric := false

	for _, update := range notification.Update {
		var name string

		value, isNumber := utils.GetNumberValues(update.Val)

		if !isNumber {
			// TODO: Set statsd data type accordingly or something
			// continue
		}

		// foundNumeric = true

		elems, keys, err := extractPrefixAndPathKeys(notification.GetPrefix(), update.GetPath())
		if err != nil {
			e.config.Log.Info().Msg(fmt.Sprintf("Failed to extract path or keys: %s", err))
		}

		name, elems = elems[len(elems)-1], elems[:len(elems)-1]

		if baseData.Measurement == "" {
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

			baseData.Measurement = pathToMetricName(measurementPath)

			point.Tags = keys

			targetConfig, found := (*e.connMgr).GetTargetConfig(point.Tags["target"])

			if found {

				deviceID, exists := targetConfig.Meta["deviceID"]
				if exists {
					point.Tags["deviceID"] = deviceID
					deviceName = path.Base(deviceID)
				} else {
					e.config.Log.Error().Msg("Device ARM ID is not set in the metadata of device" + point.Tags["target"])
					return
				}

				rackID, exists := targetConfig.Meta["rackID"]
				if exists {
					point.Tags["rackID"] = rackID
					rackName = path.Base(rackID)
				} else {
					e.config.Log.Error().Msg("Rack ARM ID is not set in the metadata of device" + point.Tags["target"])
					return
				}

				fabricID, exists := targetConfig.Meta["fabricID"]
				if exists {
					point.Tags["fabricID"] = fabricID
					fabricName = path.Base(fabricID)
				} else {
					e.config.Log.Error().Msg("Fabric ARM ID is not set in the metadata of device" + point.Tags["target"])
					return
				}

				extensionID, exists := targetConfig.Meta["extensionID"]
				if !exists {
					point.Tags["extensionID"] = extensionID
					e.config.Log.Error().Msg("Cluster Extension ID is not set in the metadata of device" + point.Tags["target"])
					return
				}

			} else {
				e.config.Log.Error().Msg("Target config not found for target: " + point.Tags["target"])
				return
			}
		}

		point.Fields[pathToMetricName(name)] = value
	}

	// if !foundNumeric {
	// 	e.config.Log.Debug().Msg("No numeric values found in notification updates. Skipping...")
	// 	return
	// }

	if baseData.Measurement == "" {
		e.config.Log.Info().Msg("Point measurement is empty. Returning.")
		return
	}

	dimNames := []string{"fabricID", "rackID", "deviceID"}
	tagKeys := make([]string, 0, len(point.Tags))
	tagVals := make([]string, 0, len(point.Tags))
	for k, v := range point.Tags {
		tagKeys = append(tagKeys, k)
		tagVals = append(tagVals, v)
	}
	dimNames = append(baseData.DimNames, tagKeys...)

	dimValues := []string{}
	dimValues = append(dimValues, tagVals...)

	dimNames = append(dimNames, []string{"fabricName", "rackName", "deviceName"}...)
	dimValues = append(dimValues, []string{fabricName, rackName, deviceName}...)

	baseData.DimNames = dimNames

	value := make(map[string]interface{})

	value["dimValues"] = dimValues

	for k, v := range point.Fields {
		value[k] = v
		switch v.(type) {
		case int, float64, float32:
			value["min"] = v
			value["max"] = v
			value["sum"] = v
		default:
			e.config.Log.Error().Msg("Received metric field with non-numeric type")
			return
		}

	}
	value["count"] = 1

	baseData.Series = append(baseData.Series, value)

	baseData.Namespace = "Interface metrics"

	metric.MetricData.Base = baseData

	metricJSON, err := json.Marshal(metric)

	if err != nil {
		e.config.Log.Error().Msg("Failed to marshal point into JSON")
		return
	}

	e.config.Log.Info().Msg(string(metricJSON))

	if err != nil {
		e.config.Log.Error().Msg(err.Error())
		return
	}

	// TODO: send metric
	// metricMap := make(map[string]string)
	// metricMap[string(metricJSON)]
	// e.client.Send(, 0)
}

func (e *StatsdExporter) Start(connMgr *connections.ConnectionManager) error {
	e.connMgr = connMgr
	// TODO: add flags for statsd connection
	e.client = statsd.New("127.0.0.1", 8125)
	e.config.Log.Info().Msg("Starting Statsd exporter.")

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
