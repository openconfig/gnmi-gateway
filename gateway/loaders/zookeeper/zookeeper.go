package zookeeper

import (
	"fmt"
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
)

// TODO Maybe add config parameters for targets & requests or some prefix
const TargetPath = "/targets"
const RequestPath = "/requests"

var _ loaders.TargetLoader = new(ZookeeperTargetLoader)

type ConnectionConfig struct {
	Addresses   []string          `yaml:"addresses"`
	Request     string            `yaml:"request"`
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
	Mode   gnmi.SubscriptionMode `yaml:"mode"`
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
	switch err {
	case zk.ErrNoNode:
		configs := &targetpb.Configuration{}
		z.config.Log.Info().Msg("Could not parse zookeeper configuration: " + err.Error())
		if exists, _, _ := z.zkClient.conn.Exists(CleanPath(TargetPath)); !exists {
			z.config.Log.Info().Msg("Creating path: " + TargetPath)
			if err := CreateFullPath(z.zkClient.conn, TargetPath, zk.WorldACL(zk.PermAll)); err != nil {
				return nil, err
			}
		}
		if exists, _, _ := z.zkClient.conn.Exists(CleanPath(RequestPath)); !exists {
			z.config.Log.Info().Msg("Creating path: " + RequestPath)
			if err := CreateFullPath(z.zkClient.conn, RequestPath, zk.WorldACL(zk.PermAll)); err != nil {
				return nil, err
			}
		}
		return configs, nil
	case nil:
		z.config.Log.Info().Msg("Successfuly retreived zookeeper configuration")
	default:
		return nil, err
	}

	configs, err := z.zookeeperToTargets(data)

	if err := target.Validate(configs); err != nil {
		return nil, fmt.Errorf("configuration in zookeeper is invalid: %v", err)
	}

	return configs, nil
}

func (z *ZookeeperTargetLoader) refreshTargetConfiguration(targetChan chan<- *connections.TargetConnectionControl) error {
	targetConfig, err := z.GetConfiguration()

	if err != nil {
		z.config.Log.Error().Err(err).Msgf("Unable to get target configuration.")
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

	z.config.Log.Info().Msgf("[ZK] Starting zookeeper target monitoring")
	if err := z.refreshTargetConfiguration(targetChan); err != nil {
		z.config.Log.Error().Err(err).Msgf("[ZK] Unable to refresh targets")
	}

	childrenPaths, _, zkChildrenEvents, err := z.zkClient.conn.ChildrenW(TargetPath)
	if err != nil {
		z.config.Log.Error().Err(err).Msgf("[ZK] Unable to create watch for children of path: %s", TargetPath)
		return err
	}
	z.config.Log.Info().Msgf("[ZK] Set watch for children of path: %s", TargetPath)

	channelMap[TargetPath] = zkChildrenEvents

	for _, path := range childrenPaths {
		watchPath := CleanPath(TargetPath) + CleanPath(path)
		_, _, eventChannel, err := z.zkClient.conn.GetW(watchPath)
		channelMap[watchPath] = eventChannel
		if err != nil {
			z.config.Log.Error().Err(err).Msgf("[ZK] Unable to set watch for target at path: '%s'", watchPath)
			return err
		} else {
			z.config.Log.Info().Msgf("[ZK] Successfuly set data watch for: %s", watchPath)
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

		case zk.EventNodeChildrenChanged:

			z.config.Log.Info().Msgf("[ZK] Received children event: %s at %s", event.Type.String(), event.Path)
			if err := z.refreshTargetConfiguration(targetChan); err != nil {
				z.config.Log.Error().Err(err).Msgf("Unable to refresh target at path: '%s' - '%s'", event.Path, err)
			}

			childrenPaths, _, zkChildrenEvents, err = z.zkClient.conn.ChildrenW(TargetPath)

			if err != nil {
				z.config.Log.Error().Err(err).Msgf("[ZK] Unable to create watch for children of path: %s", TargetPath)
				return err
			}

			channelMap[TargetPath] = zkChildrenEvents

			z.config.Log.Info().Msgf("[ZK] Set watch for children of path: %s", TargetPath)

			for _, path := range childrenPaths {
				watchPath := CleanPath(TargetPath) + CleanPath(path)

				if _, exists := channelMap[watchPath]; !exists {
					_, _, eventChannel, err := z.zkClient.conn.GetW(watchPath)
					channelMap[watchPath] = eventChannel
					if err != nil {
						z.config.Log.Error().Err(err).Msgf("[ZK] Unable to set watch for target at path: '%s'", watchPath)
						return err
					} else {
						z.config.Log.Info().Msgf("[ZK] Successfuly set data watch for: %s", watchPath)
					}
				}

			}

		case zk.EventNodeDataChanged:

			z.config.Log.Info().Msgf("[ZK] Received data event: %s at path '%s'", event.Type.String(), event.Path)

			if err := z.refreshTargetConfiguration(targetChan); err != nil {
				z.config.Log.Error().Err(err).Msgf("Unable to refresh target at path: '%s' - '%s'", event.Path, err)
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
}

func (z *ZookeeperTargetLoader) zookeeperToTargets(t *TargetConfig) (*targetpb.Configuration, error) {

	configs := &targetpb.Configuration{
		Target:  make(map[string]*targetpb.Target),
		Request: make(map[string]*gnmi.SubscribeRequest),
	}

	for connName, conn := range t.Connection {
		configs.Target[connName] = &targetpb.Target{
			Addresses: conn.Addresses,
			Request:   conn.Request,
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
				Path: path,
				Mode: request.Mode,
				SampleInterval: request.SampleInterval,
				SuppressRedundant: request.SuppressRedundant,
				HeartbeatInterval: request.HeartbeatInterval,
			})
		}
		targetName, found := getRequestTargetName(configs.Target, requestName)

		if !found {
			z.config.Log.Error().Msg("No target uses request with name: " + requestName)
			continue
		}

		configs.Request[requestName] = &gnmi.SubscribeRequest{
			Request: &gnmi.SubscribeRequest_Subscribe{
				Subscribe: &gnmi.SubscriptionList{
					Prefix: &gnmi.Path{
						Target: targetName,
					},
					Subscription: subs,
					UpdatesOnly: request.UpdatesOnly,
				},
			},
		}
	}

	return configs, nil
}

func getRequestTargetName(targets map[string]*targetpb.Target, requestName string) (string, bool) {
	for target, config := range targets {
		if config.Request == requestName {
			return target, true
		}
	}
	return "", false
}
