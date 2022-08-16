package fluentd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

const Name = "fluentd"
const MetricType = "metric"
const LogType = "log"
const LoggedMetricType = "loggedMetric"

var _ exporters.Exporter = new(FluentdExporter)

// TODO: Change struct fields according to geneva mds log reqs
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
	exporters.Register(Name, NewFluentdExporter)
}

func NewFluentdExporter(config *configuration.GatewayConfig) exporters.Exporter {
	return &FluentdExporter{
		config: config,
	}
}

type FluentdExporter struct {
	config       *configuration.GatewayConfig
	connMgr      *connections.ConnectionManager
	fluentLogger *fluent.Fluent
}

func (e *FluentdExporter) Name() string {
	return Name
}

func (e *FluentdExporter) Export(leaf *ctree.Leaf) {
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

			if e.connMgr != nil {
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
		}

		point.Fields[pathToMetricName(name)] = value

		if metric.Measurement == "" {
			e.config.Log.Info().Msg("Point measurement is empty. Returning.")
			return
		}

		metric.Dims = point.Tags
		// ns since epoch
		metric.Dims["timestamp"] = strconv.FormatInt(notification.Timestamp, 10)

		notificationType := e.config.GetPathMetadata(path)["type"]
		e.config.Log.Debug().Msgf("Notification type: [ %s ]", notificationType)

		if e.fluentLogger != nil && (notificationType == LogType || notificationType == LoggedMetricType) {
			if err := e.fluentLogger.Post(metric.Measurement, metric); err != nil {
				e.config.Log.Error().Msg("failed emmiting event log to fluentd: " + err.Error())
			}
		}
	}
}

func (e *FluentdExporter) Start(connMgr *connections.ConnectionManager) error {

	e.config.Log.Info().Msg("Starting Fluentd exporter.")

	e.connMgr = connMgr

	var err error

	if err != nil {
		return err
	}

	if e.config.Exporters.FluentHost != "" {
		// TODO: Perhaps add some exporter config parameters for config values
		e.fluentLogger, err = fluent.New(fluent.Config{
			FluentHost:             e.config.Exporters.FluentHost,
			FluentPort:             e.config.Exporters.FluentPort,
			Async:                  true,
			AsyncReconnectInterval: 500,
		})
	}

	return err
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
