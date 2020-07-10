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

// Package stats provides an interface to collect internal metrics about the
// gateway and it's components.
package stats

import (
	"go.uber.org/atomic"
	"sync"
)

var GatewayStats Stats

func init() {
	GatewayStats = NewGroup()
}

// Stats is a interface to collect internal gateway metrics.
type Stats interface {
	// Uint64 returns an *atomic.Uint64 counter if the named one exists,
	// otherwise it creates a new named counter and returns that.
	Uint64(name string) *atomic.Uint64
	// DumpUint64 will return a complete copy of all the uint64 counters
	// in a new map.
	DumpUint64() map[string]uint64
}

type Group struct {
	stats *sync.Map
}

func NewGroup() *Group {
	return &Group{
		stats: new(sync.Map),
	}
}

func (g Group) Uint64(name string) *atomic.Uint64 {
	counter, exists := g.stats.Load(name)
	if exists {
		return counter.(*atomic.Uint64)
	}
	// Load or store to catch being beaten to the insert.
	loaded, _ := g.stats.LoadOrStore(name, new(atomic.Uint64))
	return loaded.(*atomic.Uint64)
}

func (g Group) DumpUint64() map[string]uint64 {
	counters := make(map[string]uint64)
	g.stats.Range(func(key, value interface{}) bool {
		counters[key.(string)] = value.(*atomic.Uint64).Load()
		return true
	})
	return counters
}
