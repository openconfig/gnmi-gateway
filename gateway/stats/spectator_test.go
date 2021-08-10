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

package stats_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/stats"
)

// some arbitrary value to keep the math consistent
const ArbitraryValue = 2906

// Test the stats Registry that should be initialized automatically.
func TestRegistry(t *testing.T) {
	assertion := assert.New(t)

	assertion.NotNil(stats.Registry)

	wg := new(sync.WaitGroup)
	// Generate a set of stats
	for i := 1; i <= 1000; i++ {
		wg.Add(1)
		go func(i int) {
			for j := 1; j <= 1000; j++ {
				val := i * j * ArbitraryValue
				stats.Registry.Counter(strconv.Itoa(i), stats.NoTags).Add(int64(val))
			}
			wg.Done()
		}(i)
	}

	wg.Wait()

	output := stats.Registry.Meters()
	for _, meter := range output {
		name := meter.MeterId().Name()
		if strings.HasPrefix(name, "spectator.") {
			// Skip built-in measurements, only measure what we inserted
			continue
		}

		i, err := strconv.Atoi(name)
		assertion.NoError(err)
		var expected float64
		for j := 1; j <= 1000; j++ {
			val := i * j * ArbitraryValue
			expected += float64(val)
		}
		measurements := meter.Measure()
		assertion.Len(measurements, 1)
		assertion.Equal(expected, measurements[0].Value())
		if expected != measurements[0].Value() {
			// Stop early so we don't get 1000 errors
			t.FailNow()
		}
	}
}

// Test the stats Registry after calling StartSpectator.
func TestStartSpectator(t *testing.T) {
	assertion := assert.New(t)

	assertion.NotNil(stats.Registry)

	wg := new(sync.WaitGroup)
	// Generate a set of stats
	for i := 1; i <= 1000; i++ {
		wg.Add(1)
		go func(i int) {
			for j := 1; j <= 1000; j++ {
				val := i * j * ArbitraryValue
				stats.Registry.Counter(strconv.Itoa(i), stats.NoTags).Add(int64(val))
			}
			wg.Done()
		}(i)
	}

	wg.Wait()

	config := &configuration.GatewayConfig{StatsSpectatorURI: "https://localhost"}
	s, err := stats.StartSpectator(config)
	assertion.NoError(err)
	assertion.NotNil(s)
	assertion.NotNil(stats.Registry)

	output := stats.Registry.Meters()
	var correctMeters = 0
	for _, meter := range output {
		i, err := strconv.Atoi(meter.MeterId().Name())
		if err != nil {
			continue
		}
		var expected float64
		for j := 1; j <= 1000; j++ {
			val := i * j * ArbitraryValue
			expected += float64(val)
		}
		measurements := meter.Measure()
		assertion.Len(measurements, 1)
		assertion.Equal(expected, measurements[0].Value())
		if expected != measurements[0].Value() {
			// Stop early so we don't get 1000 errors
			t.FailNow()
		}
		correctMeters++
	}
	assertion.Equal(1000, correctMeters)
}
