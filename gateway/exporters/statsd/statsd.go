package statsd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"

	statsd "github.com/etsy/statsd/examples/go"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
)

const Name = "statsd"

var _ exporters.Exporter = new(StatsdExporter)

func init() {
	exporters.Register(Name, NewStatsdExporter)
}

func NewStatsdExporter(config *configuration.GatewayConfig) exporters.Exporter {
	exporter := &StatsdExporter{
		config: config,
		host:   config.Exporters.StatsdHost,
		port:   config.Exporters.StatsdPort,
	}
	return exporter
}

type StatsdExporter struct {
	cache  *cache.Cache
	config *configuration.GatewayConfig
	host   string
	port   int
}

func (e *StatsdExporter) Name() string {
	return Name
}

func (e *StatsdExporter) Export(leaf *ctree.Leaf) {
	notification := leaf.Value().(*gnmipb.Notification)
	//e.config.Log.Info().Msg(utils.GNMINotificationPrettyString(notification))
	client := statsd.New(e.host, e.port)

	// Gets a map for each update in the notification
	metricInfoMaps := utils.GetDataAsMaps(notification)

	// Parses each update included in the notification
	for _, metricInfo := range metricInfoMaps {
		formattedMetricInfo := ""
		if e.config.Exporters.EnableGenevaFormat {
			account := e.config.Exporters.GenevaAccount
			namespace := e.config.Exporters.GenevaNamespace
			// Not sure what "Metric" means for geneva
			// Is it specified by the user or provided programmatically?
			//metricName := e.config.Exporters.GenevaMetric
			// going for programmatical approach
			metricName := strings.TrimPrefix(metricInfo["path"], "/")

			if account != "" && namespace != "" && metricName != "" {
				formattedMetricInfo = GenevaFormatString(metricInfo, account, namespace, metricName)
			} else {
				e.config.Log.Error().Msg("Geneva account, namespace and metric name have to be set to use geneva format")
				e.config.Log.Error().Msg("Aborting export")
				return
			}
		} else {
			jsonMetricInfo, err := json.Marshal(metricInfo)
			if err != nil {
				e.config.Log.Error().Msg(err.Error())
			}

			formattedMetricInfo = string(jsonMetricInfo)
		}

		metric := make(map[string]string)
		metric[formattedMetricInfo] = metricInfo["value"]

		// This string will be the input for statsd
		fmt.Println(formattedMetricInfo + ":" + metricInfo["value"] + "\n")
		client.Send(metric, 1)
	}
}

func (e *StatsdExporter) Start(cache *cache.Cache) error {
	_ = cache
	e.config.Log.Info().Msg("Starting statsd exporter.")
	if e.host == "" {
		return fmt.Errorf("Failed to start statsd exporter: statsd host should not be empty")
	}
	return nil
}

// TODO: Add account, arm ID, namespace and metric name as params
func GenevaFormatString(data map[string]string, account string, namespace string, metric string) string {
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println(err.Error())
	}

	genevaData := make(map[string]string)

	genevaData["Account"] = account
	genevaData["Namespace"] = namespace
	genevaData["Metric"] = metric
	genevaData["Dims"] = string(jsonData)

	genevaJSONData, err := json.Marshal(genevaData)
	if err != nil {
		return ""
	}
	output := string(genevaJSONData)
	return output
}
