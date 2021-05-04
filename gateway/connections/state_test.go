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

package connections

import (
	"github.com/openconfig/gnmi/cache"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestConnectionState_updateTargetCache(t *testing.T) {
	state := &ConnectionState{
		//clusterMember: clusterMember,
		//config:        c.config,
		//connManager:   c,
		name:        "test_state",
		targetCache: cache.New(nil).Add("a"),
		//target:        newConfig,
		//request:       msg.Insert.Request[newConfig.Request],
		seen: make(map[string]bool),
		//useLock:       c.zkConn != nil && !noLock,
	}
	state.InitializeMetrics()

	update := []*gnmipb.Update{
		{
			Path: &gnmipb.Path{Elem: []*gnmipb.PathElem{{Name: "x"}}},
			Val:  &gnmipb.TypedValue{Value: &gnmipb.TypedValue_IntVal{IntVal: 1}},
		},
		{
			Path: &gnmipb.Path{Elem: []*gnmipb.PathElem{{Name: "y"}}},
			Val:  &gnmipb.TypedValue{Value: &gnmipb.TypedValue_IntVal{IntVal: 2}},
		},
		{
			Path: &gnmipb.Path{Elem: []*gnmipb.PathElem{{Name: "z"}}},
			Val:  &gnmipb.TypedValue{Value: &gnmipb.TypedValue_IntVal{IntVal: 3}},
		},
	}
	notifications := []struct {
		notification *gnmipb.Notification
		expected     error
	}{
		{
			&gnmipb.Notification{Timestamp: 1, Prefix: &gnmipb.Path{Target: "a"}, Update: update},
			nil,
		},
		{
			&gnmipb.Notification{Timestamp: 2, Prefix: &gnmipb.Path{Target: "a"}, Update: update},
			nil,
		},
		{
			&gnmipb.Notification{Timestamp: 3, Prefix: &gnmipb.Path{Target: "a"}, Update: update},
			nil,
		},
		{
			&gnmipb.Notification{Timestamp: 1, Prefix: &gnmipb.Path{Target: "a"}, Update: update},
			nil,
		},
	}
	for i, notificationCase := range notifications {
		notificationCase.notification.Update[0].Val.Value.(*gnmipb.TypedValue_IntVal).IntVal += int64(i)
		notificationCase.notification.Update[1].Val.Value.(*gnmipb.TypedValue_IntVal).IntVal += int64(i)
		notificationCase.notification.Update[2].Val.Value.(*gnmipb.TypedValue_IntVal).IntVal += int64(i)
		err := state.updateTargetCache(state.targetCache, notificationCase.notification)
		assert.Equal(t, notificationCase.expected, err)
	}
	assert.Equal(t, float64(3), state.counterStale.Count())
}
