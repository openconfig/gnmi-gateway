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

package exporters_test

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/exporters"
	"strconv"
	"testing"
)

func makeExampleLabels(seed int) prometheus.Labels {
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

	firstHash := exporters.NewMetricHash("test_metric", testLabels)
	for i := 0; i < 100; i++ {
		assertions.Equal(firstHash, exporters.NewMetricHash("test_metric", testLabels), "All hashes of the testLabels should be the same.")
	}
}
