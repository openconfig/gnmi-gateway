package statsd

import (
	"fmt"

	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"

	statsd "github.com/etsy/statsd/examples/go"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/exporters"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
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
	e.config.Log.Info().Msg(utils.GNMINotificationPrettyString(notification))
	client := statsd.New(e.host, e.port)

	// not proper, still in progress
	metric := make(map[string]string)
	metric["test"] = utils.GNMINotificationGenevaString(notification, "AFONFA", "MetricTutorial", "armID")[0]

	client.Send(metric, 1)
}

func (e *StatsdExporter) Start(cache *cache.Cache) error {
	_ = cache
	e.config.Log.Info().Msg("Starting statsd exporter.")
	if e.host == "" {
		return fmt.Errorf("Failed to start statsd exporter: statsd host should not be empty")
	}
	return nil
}
