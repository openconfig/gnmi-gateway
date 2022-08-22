package fluentd

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

const Name = "fluentd"
const LogType = "log"
const LoggedMetricType = "loggedMetric"

var _ exporters.Exporter = new(FluentdExporter)

type LogEntry struct {
	Namespace     string            `msg:"eventNamespace"`
	EventName     string            `msg:"eventName"`
	GnmiPath      string            `msg:"gnmiPath"`
	SubscribePath string            `msg:"subscribedPath"`
	Value         interface{}       `msg:"value"`
	Meta          map[string]string `msg:"meta"`
	FabricID      string            `msg:"fabricId"`
	RackID        string            `msg:"rackId"`
	DeviceID      string            `msg:"deviceId"`
	ExtensionID   string            `msg:"extensionId"`
	DeviceName    string            `msg:"deviceName"`
	Timestamp     string            `msg:"gnmiTimestamp"`
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
		notificationPath := utils.GetTrimmedPath(notification.Prefix, update.Path)
		notificationType := e.config.GetPathMetadata(notificationPath)["type"]

		if e.fluentLogger == nil {
			e.config.Log.Debug().Msg("fluentd client is unset")
			continue
		}

		if notificationType != LogType && notificationType != LoggedMetricType {
			e.config.Log.Debug().Msgf("received notification without log type. received %s", notificationType)
			continue
		}

		logEntry := LogEntry{}

		value, valid := utils.GetValues(update.Val)

		if !valid {
			continue
		}

		logEntry.Value = value

		elems, keys, err := extractPrefixAndPathKeys(notification.GetPrefix(), update.GetPath())
		if err != nil {
			e.config.Log.Info().Msg(fmt.Sprintf("Failed to extract notificationPath or keys: %s", err))
		}

		subscribePath := strings.Join(elems, "/")

		p := notification.GetPrefix()
		target := p.GetTarget()
		origin := p.GetOrigin()

		logEntry.SubscribePath = subscribePath
		logEntry.GnmiPath = getPathWithKeys(*update.Path)

		if origin != "" {
			keys["origin"] = origin
		}

		if target != "" {
			keys["target"] = target
		}

		logEntry.EventName = notificationPathToMetricName(subscribePath)

		logEntry.Meta = keys

		if e.connMgr != nil {
			targetName := logEntry.Meta["target"]
			targetConfig, found := (*e.connMgr).GetTargetConfig(targetName)

			if found {
				deviceID, exists := targetConfig.Meta["deviceID"]
				if exists {
					logEntry.DeviceID = deviceID
					logEntry.DeviceName = path.Base(deviceID)
				} else {
					e.config.Log.Error().Msg("Device ARM ID is not set in the metadata of target: " + targetName)
					return
				}

				logEntry.RackID, exists = targetConfig.Meta["rackID"]
				if !exists {
					e.config.Log.Error().Msg("Rack ARM ID is not set in the metadata of target: " + targetName)
					return
				}

				logEntry.FabricID, exists = targetConfig.Meta["fabricID"]
				if !exists {
					e.config.Log.Error().Msg("Fabric ARM ID is not set in the metadata of target: " + targetName)
					return
				}

				logEntry.ExtensionID = e.config.Exporters.ExtensionArmId

				// TODO: Handle possible duplicates
				// Used for aditional metadata fields
				for _, fieldName := range e.config.ExporterMetadataAllowlist {
					fieldVal, exists := targetConfig.Meta[fieldName]
					if exists {
						logEntry.Meta[fieldName] = fieldVal
					}
				}
			} else {
				e.config.Log.Error().Msg("Target config not found for target: " + logEntry.Meta["target"])
				return
			}
		}

		if logEntry.EventName == "" {
			e.config.Log.Info().Msg("Point event is empty. Returning.")
			return
		}

		// ns since epoch
		logEntry.Timestamp = strconv.FormatInt(notification.Timestamp, 10)

		logEntry.Namespace = e.config.GetPathMetadata(notificationPath)["Namespace"]
		if logEntry.Namespace == "" {
			logEntry.Namespace = "Default"
		}

		e.config.Log.Debug().Msgf("Log entry: %v", logEntry)
		if err := e.fluentLogger.Post(logEntry.Namespace, logEntry); err != nil {
			e.config.Log.Error().Msg("failed emmiting event log to fluentd: " + err.Error())
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
			MarshalAsJSON:          e.config.Exporters.FluentMarshalJSON,
			Async:                  true,
			AsyncReconnectInterval: 500,
		})
	} else {
		return errors.New("fluentd exporter was selected, but no fluentd host was provided")
	}

	return err
}

func notificationPathToMetricName(metricPath string) string {
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
		return elems, keys, errors.New("notificationPath contains no elems")
	}

	return elems, keys, nil
}

func getPathWithKeys(path gnmipb.Path) string {
	result := ""
	for _, elem := range path.Elem {
		result += "/" + elem.Name
		for key, val := range elem.GetKey() {
			result += "[" + key + "=" + val + "]"
		}
	}
	return result
}
