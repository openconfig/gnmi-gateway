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

// Copyright 2018 Google Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Portions of this file including ConnectionState and its receivers (excluding modifications) are from
// https://github.com/openconfig/gnmi/blob/89b2bf29312cda887da916d0f3a32c1624b7935f/cmd/gnmi_collector/gnmi_collector.go

package connections

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi/errlist"

	"github.com/Netflix/spectator-go"
	"github.com/Netflix/spectator-go/histogram"
	"github.com/golang/protobuf/proto"
	"github.com/openconfig/gnmi-gateway/gateway/cache"
	"github.com/openconfig/gnmi/client"
	gnmiclient "github.com/openconfig/gnmi/client/gnmi"
	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	targetpb "github.com/openconfig/gnmi/proto/target"
	"golang.org/x/sync/semaphore"

	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/locking"
	"github.com/openconfig/gnmi-gateway/gateway/stats"
	"github.com/openconfig/gnmi-gateway/gateway/utils"
)

// ConnectionState makes the calls to connect a target, tracks any associated connection state, and is the container for
// the target's cache data. It is created once for every device and used as a closure parameter by ProtoHandler.
type ConnectionState struct {
	ConnectionLockAcquired bool
	client                 *client.ReconnectClient
	clientCancel           context.CancelFunc
	clusterMember          bool
	config                 *configuration.GatewayConfig
	// connected status is set to true when the first gnmi notification is received.
	// it gets reset to false when disconnect call back of ReconnectClient is called.
	connected bool
	// connecting status is used to signal that some of the connection process has been started and
	// full reconnection is necessary if the target configuration changes
	connecting  bool
	connManager ConnectionManager
	// lock is the distributed lock that must be acquired before a connection is made if .connectWithLock() is called
	lock locking.DistributedLocker
	// The unique name of the target that is being connected to
	name string
	// noTLSWarning indicates if the warning about the NoTLS flag deprecation
	// has been displayed yet.
	noTLSWarning bool
	queryTarget  string
	request      *gnmipb.SubscribeRequest
	// seen is the list of targets that have been seen on this connection
	seen      map[string]bool
	seenMutex sync.Mutex
	// stopped status signals that .disconnect() has been called we no longer want to connect to this target so we
	// should stop trying to connect and release any locks that are being held
	stopped bool
	// synced status signals that a sync message was received from the target.
	synced      bool
	target      *targetpb.Target
	targetCache *cache.Target
	useLock     bool

	// metrics
	metricTags           map[string]string
	counterCoalesced     *spectator.Counter
	counterNotifications *spectator.Counter
	counterRejected      *spectator.Counter
	counterStale         *spectator.Counter
	counterSync          *spectator.Counter
	gaugeSynced          *spectator.Gauge
	timerLatency         *histogram.PercentileTimer
}

func (t *ConnectionState) InitializeMetrics() {
	t.metricTags = map[string]string{"gnmigateway.client.target": t.name}
	t.counterCoalesced = stats.Registry.Counter("gnmigateway.client.subscribe.coalesced", t.metricTags)
	t.counterNotifications = stats.Registry.Counter("gnmigateway.client.subscribe.notifications", t.metricTags)
	t.counterRejected = stats.Registry.Counter("gnmigateway.client.subscribe.rejected", t.metricTags)
	t.counterStale = stats.Registry.Counter("gnmigateway.client.subscribe.stale", t.metricTags)
	t.counterSync = stats.Registry.Counter("gnmigateway.client.subscribe.sync", t.metricTags)
	t.gaugeSynced = stats.Registry.Gauge("gnmigateway.client.subscribe.synced", t.metricTags)
	t.timerLatency = histogram.NewPercentileTimer(stats.Registry, "gnmigateway.client.subscribe.latency", t.metricTags)

}

// Equal returns true if the target config is different than the target config for
// this ConnectionState instance.
func (t *ConnectionState) Equal(other *targetpb.Target) bool {
	if len(t.target.Addresses) != len(other.Addresses) {
		return false
	}
	for i, addr := range t.target.Addresses {
		if other.Addresses[i] != addr {
			return false
		}
	}

	if t.target.Credentials == nil && other.Credentials != nil || t.target.Credentials != nil && other.Credentials == nil {
		return false
	}

	if t.target.Credentials != nil && other.Credentials != nil {
		if t.target.Credentials.Username != other.Credentials.Username {
			return false
		}
		if t.target.Credentials.Password != other.Credentials.Password {
			return false
		}
	}
	return true
}

// Seen returns true if the named target has been seen on this connection.
func (t *ConnectionState) Seen(target string) bool {
	t.seenMutex.Lock()
	defer t.seenMutex.Unlock()
	return t.seen[target]
}

func (t *ConnectionState) doConnect() {
	t.connecting = true
	t.config.Log.Info().Msgf("Target %s: Connecting", t.name)
	query, err := client.NewQuery(t.request)
	if err != nil {
		t.config.Log.Error().Msgf("Target %s: unable to create query: NewQuery(%s): %v", t.name, t.request.String(), err)
		return
	}
	query.Addrs = t.target.Addresses

	if t.target.Credentials != nil {
		query.Credentials = &client.Credentials{
			Username: t.target.Credentials.Username,
			Password: t.target.Credentials.Password,
		}
	}

	// TODO (cmcintosh): make PR for targetpb to include TLS config and remove this
	_, NoTLS := t.target.Meta["NoTLS"]
	if NoTLS && !t.noTLSWarning {
		t.noTLSWarning = true
		t.config.Log.Warn().Msg("DEPRECATED: The 'NoTLS' target flag has been deprecated and will be removed in a future release. Please use 'NoTLSVerify' instead.")
	}

	_, NoTLSVerify := t.target.Meta["NoTLSVerify"]

	// TLS is always enabled for localTargets but we won't verify certs if no client TLS config exists.
	if t.config.ClientTLSConfig != nil && !NoTLS && !NoTLSVerify {
		query.TLS = t.config.ClientTLSConfig
	} else {
		query.TLS = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	var prefixTarget string
	if t.request.GetSubscribe() != nil && t.request.GetSubscribe().GetPrefix() != nil {
		prefixTarget = t.request.GetSubscribe().GetPrefix().GetTarget()
	}

	if prefixTarget != "" {
		query.Target = prefixTarget
		t.queryTarget = prefixTarget
	} else {
		query.Target = t.name
		t.queryTarget = t.name
	}

	query.Timeout = t.config.TargetDialTimeout

	query.ProtoHandler = t.handleUpdate

	if err := query.Validate(); err != nil {
		t.config.Log.Error().Err(err).Msgf("query.Validate(): %v", err)
		return
	}

	var ctx context.Context
	ctx, t.clientCancel = context.WithCancel(context.Background())
	t.config.Log.Info().Msgf("Target %s: Subscribing", t.name)
	t.client = client.Reconnect(&client.BaseClient{}, t.disconnected, t.reset)
	if err := t.client.Subscribe(ctx, query, gnmiclient.Type); err != nil {
		t.config.Log.Info().Msgf("Target %s: Subscribe stopped: %v", t.name, err)
	}
}

// Attempt to acquire a connection slot and connect to the target. If ConnectionState.disconnect() is called
// all attempts and connections are aborted.
func (t *ConnectionState) connect(connectionSlot *semaphore.Weighted) {
	var connectionSlotAcquired = false
	for !t.stopped {
		if !connectionSlotAcquired {
			connectionSlotAcquired = connectionSlot.TryAcquire(1)
		}
		if connectionSlotAcquired {
			t.doConnect()
		}
	}
	if connectionSlotAcquired {
		connectionSlot.Release(1)
	}
}

// Attempt to acquire a connection slot. After a connection slot is acquired attempt to grab the lock for the target.
// After the lock for the target is acquired connect to the target. If ConnectionState.disconnect() is called
// all attempts and connections are aborted.
func (t *ConnectionState) connectWithLock(connectionSlot *semaphore.Weighted) {
	var connectionSlotAcquired = false
	for !t.stopped {
		if !connectionSlotAcquired {
			t.config.Log.Info().Msgf("Target %s: Acquiring connection slot", t.name)
			connectionSlotAcquired = connectionSlot.TryAcquire(1)
		}
		if connectionSlotAcquired {
			if !t.ConnectionLockAcquired {
				t.config.Log.Info().Msgf("Target %s: Acquiring lock", t.name)
				var err error
				t.ConnectionLockAcquired, err = t.lock.Try()
				if err != nil {
					t.config.Log.Error().Msgf("Target %s: error while trying to acquire lock: %v", t.name, err)
					time.Sleep(2 * time.Second)
				}
			}
			if t.ConnectionLockAcquired {
				t.config.Log.Info().Msgf("Target %s: Lock acquired", t.name)
				t.doConnect()
				if t.lock.LockAcquired() {
					err := t.lock.Unlock()
					if err != nil && err != zk.ErrNotLocked {
						t.config.Log.Error().Msgf("Target %s: error while releasing lock: %v", t.name, err)
					}
				}
				t.ConnectionLockAcquired = false
				t.config.Log.Info().Msgf("Target %s: Lock released", t.name)
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}
	t.config.Log.Info().Msgf("Target %s: Stopped", t.name)
	if connectionSlotAcquired {
		connectionSlot.Release(1)
	}
}

// Disconnect from the target or stop trying to connect.
func (t *ConnectionState) disconnect() error {
	t.config.Log.Info().Msgf("Target %s: Disconnecting", t.name)
	t.stopped = true
	return t.client.Close() // this will disconnect and reset the cache via the disconnect callback
}

// reset is the callback for gNMI client to signal that it will reconnect.
func (t *ConnectionState) reset() {
	t.config.Log.Info().Msgf("Target %s: gNMI client will reconnect", t.name)
}

// Callback for gNMI client to signal that it has disconnected.
func (t *ConnectionState) disconnected() {
	t.connected = false
	t.synced = false
	t.seenMutex.Lock()
	t.seen = map[string]bool{}
	t.seenMutex.Unlock()
	if !t.isWildcardTarget() {
		t.targetCache.Reset()
	}
	t.config.Log.Info().Msgf("Target %s: Disconnected", t.name)
}

func (t *ConnectionState) reconnect(connectionSlot *semaphore.Weighted) error {
	t.config.Log.Info().Msgf("Target %s: Reconnecting", t.name)
	// if t.connecting
	if err := t.client.Close(); err != nil {
		return err
	}

	go t.connect(connectionSlot)
	return nil
}

func (t *ConnectionState) unlock() error {
	t.config.Log.Info().Msgf("Target %s: Unlocking", t.name)
	t.clientCancel()
	return nil
	//return t.client.Close()
}

func (t *ConnectionState) isWildcardTarget() bool {
	return utils.IsWildcardTarget(t.queryTarget)
}

// handleUpdate parses a protobuf message received from the targetCache. This implementation handles only
// gNMI SubscribeResponse messages. When the message is an Update, the GnmiUpdate method of the
// cache.Target is called to generate an update. If the message is a sync_response, then targetCache is
// marked as synchronised.
func (t *ConnectionState) handleUpdate(msg proto.Message) error {
	//fmt.Printf("%+v\n", msg)
	t.counterNotifications.Increment()
	if !t.connected {
		if !t.isWildcardTarget() {
			t.targetCache.Connect()
		}
		t.connected = true
		t.config.Log.Info().Msgf("Target %s: Connected", t.name)
	}
	resp, ok := msg.(*gnmipb.SubscribeResponse)
	if !ok {
		return fmt.Errorf("failed to type assert message %#v", msg)
	}
	switch v := resp.Response.(type) {
	case *gnmipb.SubscribeResponse_Update:
		if t.rejectUpdate(v.Update) {
			t.counterRejected.Increment()
			return nil
		}

		if t.synced {
			for _, u := range v.Update.Update {
				t.counterCoalesced.Add(int64(u.Duplicates))
			}
			t.timerLatency.Record(time.Duration(time.Now().UnixNano() - v.Update.Timestamp))
		}

		if t.isWildcardTarget() {
			targetCache := t.connManager.Cache().GetTarget(v.Update.Prefix.Target)
			if targetCache == nil {
				targetCache = t.connManager.Cache().Add(v.Update.Prefix.Target)
			}
			targetCache.SetEmitStaleUpdates(t.config.EmitStaleUpdates)
			t.seenMutex.Lock()
			t.seen[v.Update.Prefix.Target] = true
			t.seenMutex.Unlock()
			err := t.updateTargetCache(targetCache, v.Update)
			if err != nil {
				return err
			}
		} else {
			// Gracefully handle gNMI implementations that do not set Prefix.Target in their
			// SubscribeResponse Updates.
			if v.Update.GetPrefix() == nil {
				v.Update.Prefix = &gnmipb.Path{}
			}
			if v.Update.Prefix.Target == "" {
				v.Update.Prefix.Target = t.queryTarget
			}
			t.targetCache.SetEmitStaleUpdates(t.config.EmitStaleUpdates)
			err := t.updateTargetCache(t.targetCache, v.Update)
			if err != nil {
				return err
			}
		}
	case *gnmipb.SubscribeResponse_SyncResponse:
		t.sync()
		if !t.isWildcardTarget() {
			t.targetCache.Sync()
		}
	case *gnmipb.SubscribeResponse_Error:
		return fmt.Errorf("error in response: %s", v)
	default:
		return fmt.Errorf("unknown response %T: %v", v, v)
	}
	return nil
}

// sync sets the state of the ConnectionState to synced.
func (t *ConnectionState) sync() {
	t.config.Log.Info().Msgf("Target %s: Synced", t.name)
	t.synced = true
	t.counterSync.Increment()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for t.synced {
			select {
			case <-ticker.C:
				t.gaugeSynced.Set(1)
			}
		}
	}()
}

func (t *ConnectionState) updateTargetCache(cache *cache.Target, update *gnmipb.Notification) error {
	var hasError bool
	err := cache.GnmiUpdate(update)
	if err != nil {
		// Some errors won't corrupt the cache so no need to return an error to the ProtoHandler caller. For these
		// errors we just log them and move on.
		errList, isList := err.(errlist.Error)
		if isList {
			for _, err := range errList.Errors() {
				hasError = t.handleCacheError(err)
				if hasError {
					break
				}
			}
		} else {
			hasError = t.handleCacheError(err)
		}
		if hasError {
			return fmt.Errorf("target '%s' cache update error: %v: %+v", t.name, err, utils.GNMINotificationPrettyString(update))
		}
	}
	return nil
}

func (t *ConnectionState) handleCacheError(err error) bool {
	switch err.Error() {
	case "suppressed duplicate value":
	case "update is stale":
		t.counterStale.Increment()
		//t.config.Log.Warn().Msgf("Target %s: %s: %s", t.name, err, utils.GNMINotificationPrettyString(update))
		return false
	}
	return true
}

// rejectUpdate returns true if the gNMI notification is unwanted based on the RejectUpdates
// configuration in GatewayConfig.
func (t *ConnectionState) rejectUpdate(notification *gnmipb.Notification) bool {
	for _, update := range notification.GetUpdate() {
		path := update.GetPath().GetElem()
		for _, rejectionPath := range t.config.UpdateRejections {
			if matchPath(path, rejectionPath) {
				return true
			}
		}
	}
	return false
}

// Return true if all of the elements in toMatch are found in path.
func matchPath(path []*gnmipb.PathElem, toMatch []*gnmipb.PathElem) bool {
	if len(path) < len(toMatch) {
		return false
	}
	for i, elem := range toMatch {
		if path[i].Name != elem.Name {
			return false
		}
		if elem.Key != nil {
			if path[i].Key == nil {
				return false
			}

			for k, v := range elem.Key {
				ov, exists := path[i].Key[k]
				if !exists {
					return false
				}
				if v != ov {
					return false
				}
			}
		}
	}
	return true
}
