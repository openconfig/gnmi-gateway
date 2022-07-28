package zookeeper

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/google/gnxi/utils/xpath"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/openconfig/gnmi-gateway/gateway/loaders"
	"github.com/openconfig/gnmi/proto/gnmi"
	targetpb "github.com/openconfig/gnmi/proto/target"
	"github.com/openconfig/gnmi/target"
	"k8s.io/utils/strings/slices"
)

// TODO Maybe add config parameters for targets & requests or some prefix
const TargetPath = "/targets"
const RequestPath = "/requests"
const CertificatePath = "/certificates"
const GlobalCertPath = "/certificates/global"

var _ loaders.TargetLoader = new(ZookeeperTargetLoader)

type CertificateConfig struct {
	ClientCA   string `yaml:"clientCa,omitempty"`
	ClientCert string `yaml:"clientCert,omitempty"`
	ClientKey  string `yaml:"clientKey,omitempty"`
}

type ConnectionConfig struct {
	Addresses   []string          `yaml:"addresses"`
	Meta        map[string]string `yaml:"meta"`
	Credentials CredentialsConfig `yaml:"credentials"`
}

type CredentialsConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type RequestConfig struct {
	Target string   `yaml:"target"`
	Paths  []string `yaml:"paths"`
	// Subscription mode to be used:
	// 0: SubscriptionMode_TARGET_DEFINED - The target selects the relevant
	// 										mode for each element.
	// 1: SubscriptionMode_ON_CHANGE - The target sends an update on element
	//                                 value change.
	// 2: SubscriptionMode_SAMPLE - The target samples values according to the
	//                              interval.
	Mode gnmi.SubscriptionMode `yaml:"mode"`
	// ns between samples in SAMPLE mode.
	SampleInterval uint64 `yaml:"sampleInterval"`
	// Indicates whether values that have not changed should be sent in a SAMPLE
	// subscription.
	SuppressRedundant bool `yaml:"supressRedundant"`
	// Specifies the maximum allowable silent period in nanoseconds when
	// suppress_redundant is in use. The target should send a value at least once
	// in the period specified.
	HeartbeatInterval uint64 `yaml:"heartbeatInterval"`
	// An optional field to specify that only updates to current state should be
	// sent to a client. If set, the initial state is not sent to the client but
	// rather only the sync message followed by any subsequent updates to the
	// current state. For ONCE and POLL modes, this causes the server to send only
	// the sync message (Sec. 3.5.2.3).
	UpdatesOnly bool `yaml:"updatesOnly"`
}

type TargetConfig struct {
	Connection map[string]ConnectionConfig `yaml:"connection"`
	Request    map[string]RequestConfig    `yaml:"request"`
}

type ZookeeperTargetLoader struct {
	config   *configuration.GatewayConfig
	hosts    []string
	interval time.Duration
	last     *targetpb.Configuration
	zkClient *ZookeeperClient
}

func init() {
	loaders.Register("zookeeper", NewZookeeperTargetLoader)
}

func (z *ZookeeperTargetLoader) Start() error {
	zClient, err := NewZookeeperClient(z.hosts, z.interval)
	if err != nil {
		return err
	}
	z.zkClient = zClient

	_, err = z.GetConfiguration() // make sure there are no errors at startup
	return err
}

func NewZookeeperTargetLoader(config *configuration.GatewayConfig) loaders.TargetLoader {
	return &ZookeeperTargetLoader{
		config:   config,
		hosts:    config.ZookeeperHosts,
		interval: config.TargetLoaders.ZookeeperReloadInterval,
	}
}

func (z *ZookeeperTargetLoader) GetConfiguration() (*targetpb.Configuration, error) {
	data, err := z.zkClient.GetZookeeperData(z.config.Log)

	if err != nil {
		return nil, err
	}

	configs, err := z.zookeeperToTargets(data)

	if err != nil {
		return nil, err
	}

	if err := target.Validate(configs); err != nil {
		return nil, fmt.Errorf("configuration in zookeeper is invalid: %v", err)
	}

	return configs, nil
}

func (z *ZookeeperTargetLoader) refreshTargetConfiguration(targetChan chan<- *connections.TargetConnectionControl) error {
	targetConfig, err := z.GetConfiguration()

	if err != nil {
		z.config.Log.Error().Err(err).Msgf("Unable to get target configuration.")
		return err
	} else {
		controlMsg := new(connections.TargetConnectionControl)
		if z.last != nil {
			for targetName := range z.last.Target {
				_, exists := targetConfig.Target[targetName]
				if !exists {
					controlMsg.Remove = append(controlMsg.Remove, targetName)
				}
			}
		}
		controlMsg.Insert = targetConfig
		z.last = targetConfig

		targetChan <- controlMsg
	}
	return nil
}

func buildEventChanArray(channelMap map[string]<-chan zk.Event) []<-chan zk.Event {
	eventChannels := []<-chan zk.Event{}
	for _, ch := range channelMap {
		eventChannels = append(eventChannels, ch)
	}
	return eventChannels
}

func (z *ZookeeperTargetLoader) WatchConfiguration(targetChan chan<- *connections.TargetConnectionControl) error {
	channelMap := make(map[string]<-chan zk.Event)

	if err := z.refreshGlobalConfig(z.config.Log, targetChan, z.config); err != nil {
		z.config.Log.Error().Err(err).Msgf("[ZK] Unable to refresh config")
		if err == zk.ErrConnectionClosed {
			goto Exit
		}
	}
	z.config.Log.Info().Msgf("[ZK] Starting zookeeper target monitoring")
	if err := z.refreshTargetConfiguration(targetChan); err != nil {
		z.config.Log.Error().Err(err).Msgf("[ZK] Unable to refresh targets")
		if err == zk.ErrConnectionClosed {
			goto Exit
		}
	}

	for _, watchPath := range []string{TargetPath, RequestPath, CertificatePath} {
		if err := z.refreshWatches(channelMap, watchPath); err != nil {
			z.config.Log.Error().Err(err).Msgf("[ZK] Unable to refresh watches")
			if err == zk.ErrConnectionClosed {
				goto Exit
			}
		}
	}

	for {
		eventChannels := buildEventChanArray(channelMap)

		cases := make([]reflect.SelectCase, len(eventChannels))

		for i, channel := range eventChannels {
			cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(channel)}
		}

		chosen, value, ok := reflect.Select(cases)
		if !ok {
			// The chosen channel has been closed, so zero out the channel to disable the case
			cases[chosen].Chan = reflect.ValueOf(nil)
		}

		event := value.Interface().(zk.Event)

		switch event.Type {
		case zk.EventNodeDeleted:

			z.config.Log.Info().Msgf("[ZK] Received event: %s at %s", event.Type.String(), event.Path)

			delete(channelMap, event.Path)

			if slices.Contains([]string{TargetPath, RequestPath, CertificatePath}, event.Path) {
				z.config.Log.Info().Msg("[ZK] Detected deletion of root config path, recreating: " + event.Path)
				if err := z.zkClient.createNode(event.Path); err != nil {
					z.config.Log.Error().Err(err).Msgf("[ZK] Unable to recreate node ", event.Path)
				}
				if err := z.refreshWatches(channelMap, event.Path); err != nil {
					z.config.Log.Error().Err(err).Msgf("[ZK] Unable to refresh watches")
					if err == zk.ErrConnectionClosed {
						goto Exit
					}
				}
			}

		case zk.EventNodeChildrenChanged:

			z.config.Log.Info().Msgf("[ZK] Received children event: %s at %s", event.Type.String(), event.Path)

			if strings.Contains(event.Path, CertificatePath) {
				if err := z.refreshGlobalConfig(z.config.Log, targetChan, z.config); err != nil {
					z.config.Log.Error().Err(err).Msgf("Unable to refresh config at path: '%s' - '%s'", event.Path, err)
					if err == zk.ErrConnectionClosed {
						goto Exit
					}
				}
			} else {
				if err := z.refreshTargetConfiguration(targetChan); err != nil {
					z.config.Log.Error().Err(err).Msgf("Unable to refresh node at path: '%s' - '%s'", event.Path, err)
					if err == zk.ErrConnectionClosed {
						goto Exit
					}
				}
			}
			if err := z.refreshWatches(channelMap, event.Path); err != nil {
				z.config.Log.Error().Err(err).Msgf("[ZK] Unable to refresh watches")
				if err == zk.ErrConnectionClosed {
					goto Exit
				}
			}

		case zk.EventNodeDataChanged:

			z.config.Log.Info().Msgf("[ZK] Received data event: %s at path '%s'", event.Type.String(), event.Path)

			if strings.Contains(event.Path, CertificatePath) {
				if err := z.refreshGlobalConfig(z.config.Log, targetChan, z.config); err != nil {
					z.config.Log.Error().Err(err).Msgf("Unable to refresh config at path: '%s' - '%s'", event.Path, err)
					if err == zk.ErrConnectionClosed {
						goto Exit
					}
				}
			} else {
				if err := z.refreshTargetConfiguration(targetChan); err != nil {
					z.config.Log.Error().Err(err).Msgf("Unable to refresh target at path: '%s' - '%s'", event.Path, err)
					if err == zk.ErrConnectionClosed {
						goto Exit
					}
				}
			}

			z.config.Log.Info().Msgf("[ZK] Edited configuration from zNode at path: %s/", event.Path)

			_, _, eventChannel, err := z.zkClient.conn.GetW(event.Path)
			if err != nil {
				z.config.Log.Error().Err(err).Msgf("Unable to create watch for zNnode at path: %s", event.Path)
				return err
			}
			channelMap[event.Path] = eventChannel

			z.config.Log.Info().Msgf("[ZK] Successfuly set data watch for: %s", event.Path)
		}
	}
Exit:
	fmt.Println("Zookeeper connection closed")
	return nil
}

func (z *ZookeeperTargetLoader) refreshWatches(channelMap map[string]<-chan zk.Event, path string) error {
	z.config.Log.Info().Msgf("Refreshing children and data watches for: '%s'", path)
	cleanPath := CleanPath(path)
	childrenPaths, _, zkChildrenEvents, err := z.zkClient.conn.ChildrenW(cleanPath)

	if err == zk.ErrNoNode {
		if err := z.zkClient.createNode(cleanPath); err != nil {
			return err
		}
		_, _, zkChildrenEvents, err = z.zkClient.conn.ChildrenW(CertificatePath)
	}

	if err != nil {
		z.config.Log.Error().Err(err).Msgf("[ZK] Unable to create watch for children of path: %s", cleanPath)
		return err
	}
	z.config.Log.Info().Msgf("[ZK] Set watch for children of path: %s", cleanPath)

	channelMap[cleanPath] = zkChildrenEvents

	for _, path := range childrenPaths {
		watchPath := cleanPath + CleanPath(path)
		_, _, eventChannel, err := z.zkClient.conn.GetW(watchPath)
		channelMap[watchPath] = eventChannel
		if err != nil {
			z.config.Log.Error().Err(err).Msgf("[ZK] Unable to set watch for node at path: '%s'", watchPath)
			return err
		} else {
			z.config.Log.Info().Msgf("[ZK] Successfuly set data watch for: %s", watchPath)
		}
	}
	return nil
}

func (z *ZookeeperTargetLoader) zookeeperToTargets(t *TargetConfig) (*targetpb.Configuration, error) {

	configs := &targetpb.Configuration{
		Target:  make(map[string]*targetpb.Target),
		Request: make(map[string]*gnmi.SubscribeRequest),
	}

	for connName, conn := range t.Connection {
		if err := ValidateConnection(&conn); err != nil {
			z.config.Log.Error().Msgf("Discarding configuration for target "+connName+": ", err)
			continue
		}
		configs.Target[connName] = &targetpb.Target{
			Addresses: conn.Addresses,
			Request:   connName,
			Meta:      conn.Meta,
			Credentials: &targetpb.Credentials{
				Username: conn.Credentials.Username,
				Password: conn.Credentials.Password,
			},
		}
	}

	for requestName, request := range t.Request {
		var subs []*gnmi.Subscription
		for _, x := range request.Paths {

			var origin = ""
			split := strings.Split(x, ":")
			if len(split) > 1 {
				origin = split[0]
				x = split[1]
			}

			path, err := xpath.ToGNMIPath(x)
			if err != nil {
				return nil, fmt.Errorf("unable to parse zookeeper config XPath: %v", err)
			}
			if origin != "" {
				path.Origin = origin
			}
			subs = append(subs, &gnmi.Subscription{
				Path:              path,
				Mode:              request.Mode,
				SampleInterval:    request.SampleInterval,
				SuppressRedundant: request.SuppressRedundant,
				HeartbeatInterval: request.HeartbeatInterval,
			})
		}
		found := false
		for targetName := range configs.Target {
			// TODO: consider adding a helper
			if match, _ := filepath.Match(request.Target, targetName); match {
				found = true
				z.config.Log.Info().Msgf("Updating target subscription. "+
					"Target: %s. Request: %s. Request target pattern: %s.",
					targetName, requestName, request.Target)

				// The gnmi client library expects one request per target. However,
				// the zookeeper loader allows multiple requests per target.
				// We'll aggregate all the requests that match a specific target.
				//
				// Let's take the following example:
				//
				// requests:
				// - name: ce_metrics
				//   target: ce*
				//   mode: 2  # (sample)
				// - name: ce_events
				//   target: ce*
				//   mode: 1  # (on change)
				//
				// targets: ce1, ce2
				//
				// In the above example, we'll generate two identical internal
				// requests by merging "ce_metrics" and "ce_events", one for
				// each of the "ce1" and "ce2" targets.
				var targetRequest *gnmi.SubscribeRequest
				var foundSub bool
				if targetRequest, foundSub = configs.Request[targetName]; !foundSub {
					targetRequest = &gnmi.SubscribeRequest{
						Request: &gnmi.SubscribeRequest_Subscribe{
							Subscribe: &gnmi.SubscriptionList{
								Prefix: &gnmi.Path{
									Target: targetName,
								},
								Subscription: subs,
								// TODO: figure out what's the expected behavior
								// of the "UpdatesOnly" flag, considering that
								// we're merging requests.
								UpdatesOnly: request.UpdatesOnly,
							},
						},
					}
					configs.Request[targetName] = targetRequest
				} else {
					targetRequest.GetSubscribe().Subscription = append(
						targetRequest.GetSubscribe().Subscription,
						subs...,
					)
				}
				z.config.Log.Info().Msgf("Updated target subscription. "+
					"Target: %s. Request: %s. Request target pattern: %s. "+
					"Subscription count: %d, subscriptions: %v",
					targetName, requestName, request.Target,
					len(configs.Request[targetName].GetSubscribe().Subscription),
					targetRequest.GetSubscribe().Subscription)

			}
		}
		if !found {
			z.config.Log.Info().Msg("No matching target for request: " + requestName)
		}
	}

	return configs, nil
}

func (z *ZookeeperTargetLoader) Close() error {
	z.zkClient.conn.Close()
	return nil
}
