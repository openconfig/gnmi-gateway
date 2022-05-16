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

// Package connections provides the connection manager that forms the transport connection
// to gNMI targets and initiates gNMI RPC calls.
//
// The ConnectionManager listens for TargetConnectionControl messages on the TargetControlChan
// and will perform the corresponding connects and disconnects for Insert and Remove, respectively.
// When an Insert is received the ConnectionManager will attempt to connect to the target if there
// is a connection slot available. The number of connection slots is configurable with the
// TargetLimit configuration parameter. If clustering is enabled the connection manager
// will attempt to acquire a lock for each target before making the connection. Locking will not be
// attempted if there are no connection slots available. Targets present in the Insert field will
// disconnect and overwrite existing targets with the same name, if the target configuration has
// changed.
//
// The ConnectionManager accepts target configurations that follow the target.proto found here:
// https://github.com/openconfig/gnmi/blob/master/proto/target/target.proto
// The ConnectionManager additionally supports some per-target meta configuration options:
//		NoTLS	- Set this field to disable TLS for the target. If client TLS credentials
//				  are not provided this field will have no effect.
//		NoLock	- Set this field to disable locking for the target. If clustering is not
//				  enabled this field will have no effect.
package connections

import (
	"github.com/openconfig/gnmi/cache"
	targetpb "github.com/openconfig/gnmi/proto/target"
)

// ConnectionManager provides an interface for connecting/disconnecting to/from
// gNMI targets and forwards updates to a gNMI cache.
// Start must be called to start listening for changes on the TargetControlChan.
type ConnectionManager interface {
	// Cache returns the *cache.Cache that contains gNMI Notifications.
	Cache() *cache.Cache
	// Forwardable returns true if this instance of the ConnectionManager
	// holds the lock for a non-cluster member connection for the named target.
	Forwardable(target string) bool
	// Start will start the loop to listen for TargetConnectionControl messages
	// on TargetControlChan.
	Start() error
	// TargetControlChan returns an input channel for TargetConnectionControl
	// messages.
	TargetControlChan() chan<- *TargetConnectionControl

	GetTargetConfig(targetName string) (targetpb.Target, bool)
}

// TargetConnectionControl messages are used to insert/update and remove targets in
// the ConnectionManager via the TargetControlChan channel.
type TargetConnectionControl struct {
	// Insert will insert and connect all of the named target configs. If a config with the same name
	// already exists it will be overwritten and the target reconnected if the new config is different
	// from the old one.
	Insert *targetpb.Configuration
	// Remove will remove and disconnect from all of the named targets.
	Remove []string
}

// InsertCount is the number of targets in the Insert field, if Insert is not nil.
func (t *TargetConnectionControl) InsertCount() int {
	if t.Insert == nil || t.Insert.Target == nil {
		return 0
	}
	return len(t.Insert.Target)
}

// RemoveCount is the number of targets in the Remove field, if Remove is not nil.
func (t *TargetConnectionControl) RemoveCount() int {
	return len(t.Remove)
}
