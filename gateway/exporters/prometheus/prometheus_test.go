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

package prometheus

import (
	"math/rand"
	"strconv"
	"testing"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi/ctree"
	pb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus/promauto"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/openconfig/gnmi-gateway/gateway/exporters"
)

var _ exporters.Exporter = new(PrometheusExporter)

func makeExampleLabels(seed int) prom.Labels {
	rand.Seed(int64(seed))
	newMap := make(map[string]string)
	for i := 0; i < 12; i++ {
		a := strconv.Itoa(rand.Int())
		b := strconv.Itoa(rand.Int())
		newMap[a] = b
	}
	return newMap
}

func TestMapHash(t *testing.T) {
	assertions := assert.New(t)

	testLabels := makeExampleLabels(2906) // randomly selected consistent seed

	firstHash := NewStringMapHash("test_metric", testLabels)
	for i := 0; i < 100; i++ {
		assertions.Equal(firstHash, NewStringMapHash("test_metric", testLabels), "All hashes of the testLabels should be the same.")
	}
}

func TestPrometheusExporter_Export(t *testing.T) {
	n := &pb.Notification{
		Prefix: &pb.Path{Target: "a", Origin: "b"},
		Update: []*pb.Update{
			{
				Path: &pb.Path{
					Elem: []*pb.PathElem{{Name: "c"}},
				},
				Val: &pb.TypedValue{Value: &pb.TypedValue_IntVal{IntVal: -1}},
			},
		},
	}

	metricName, labels := UpdateToMetricNameAndLabels(n.GetPrefix(), n.Update[0])
	metricHash := NewStringMapHash(metricName, labels)

	// Prime the delta calculator
	calc := NewDeltaCalculator()
	calc.Calc(metricHash, 20)

	var connMgr connections.ConnectionManager
	e := &PrometheusExporter{
		config:    &configuration.GatewayConfig{},
		connMgr:   &connMgr,
		deltaCalc: calc,
		metrics: map[Hash]prom.Metric{
			metricHash: promauto.NewCounter(prom.CounterOpts{
				Name:        metricName,
				ConstLabels: labels,
			}),
		},
		typeLookup: nil,
	}
	assert.NotPanics(t, func() {
		e.Export(ctree.DetachedLeaf(n))
	})

}
