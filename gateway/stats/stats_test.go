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
	"github.com/openconfig/gnmi-gateway/gateway/stats"
	"github.com/stretchr/testify/assert"
	"strconv"
	"sync"
	"testing"
)

// some arbitrary value to keep the math consistent
const ArbitraryValue = 2906

func TestGroup_Uint64(t *testing.T) {
	assertion := assert.New(t)

	var s stats.Stats
	s = stats.NewGroup()
	assertion.NotNil(s)

	wg := new(sync.WaitGroup)
	// Generate a set of stats
	for i := 1; i <= 1000; i++ {
		wg.Add(1)
		go func(i int) {
			for j := 1; j <= 1000; j++ {
				val := i * j * ArbitraryValue
				s.Uint64(strconv.Itoa(i)).Add(uint64(val))
			}
			wg.Done()
		}(i)
	}

	wg.Wait()

	output := s.DumpUint64()
	for k, v := range output {
		i, err := strconv.Atoi(k)
		assertion.NoError(err)
		var expected uint64
		for j := 1; j <= 1000; j++ {
			val := i * j * ArbitraryValue
			expected += uint64(val)
		}
		assertion.Equal(expected, v)
		if expected != v {
			// Stop early so we don't get 1000 errors
			t.FailNow()
		}
	}
}
