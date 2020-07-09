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
	"github.com/cespare/xxhash/v2"
	"sort"
	"sync"
)

func NewDeltaCalculator() *DeltaCalculator {
	return &DeltaCalculator{
		history: make(map[Hash]float64),
	}
}

type DeltaCalculator struct {
	lock    sync.Mutex
	history map[Hash]float64
}

// Calculate the delta for given hash and value. Returns the provided value and false if a previous value didn't exist.
func (d *DeltaCalculator) Calc(hash Hash, newValue float64) (float64, bool) {
	d.lock.Lock()
	defer d.lock.Unlock()
	oldValue, exists := d.history[hash]
	d.history[hash] = newValue
	return newValue - oldValue, exists
}

type Hash uint64

func NewStringMapHash(name string, labels map[string]string) Hash {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var hash string
	for _, k := range keys {
		hash += k
		hash += labels[k]
	}
	return Hash(xxhash.Sum64String(name + hash))
}
