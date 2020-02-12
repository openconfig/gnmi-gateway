// Copyright 2020 Netflix Inc
// Author: Colin McIntosh (colin@netflix.com)

package exporters

import (
	"github.com/openconfig/gnmi/cache"
	"github.com/openconfig/gnmi/ctree"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/rs/zerolog/log"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway"
	"strings"
)

func NewPrometheusExporter(config *gateway.GatewayConfig, cache *cache.Cache) Exporter {
	return &PrometheusExporter{
		config: config,
		cache:  cache,
	}
}

type PrometheusExporter struct {
	config *gateway.GatewayConfig
	cache  *cache.Cache
}

func (e *PrometheusExporter) Export(leaf *ctree.Leaf) {
	update := leaf.Value().(*gnmipb.Notification)

	UpdateToMetricNameAndTags(update)
}

func (e *PrometheusExporter) Start() {
	e.cache.SetClient(e.Export)
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

func UpdateToMetricNameAndTags(notification *gnmipb.Notification) {
	for _, update := range notification.Update {
		value, isNumber := GetNumberValues(update.Val)
		if !isNumber {
			return
		}

		metricName := ""
		tags := make(map[string]string)
		for _, elem := range update.Path.Elem {
			if metricName == "" {
				metricName = elem.Name
			} else {
				metricName = metricName + "." + elem.Name
			}

			for key, value := range elem.Key {
				tagKey := metricName + "." + key
				tags[tagKey] = value
			}
		}
		metricName = strings.TrimLeft(metricName, ".")
		log.Info().Msgf("%s %+v %f", metricName, tags, value)
	}
}
