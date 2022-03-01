package zookeeper

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ZookeeperClient struct {
	acl  []zk.ACL
	conn *zk.Conn
}

func (z *ZookeeperClient) GetZookeeperData(log zerolog.Logger) (*TargetConfig, error) {
	connectionMap := make(map[string]ConnectionConfig)
	requestMap := make(map[string]RequestConfig)

	zNodes, _, err := z.conn.Children(CleanPath(TargetPath))
	if err != nil {
		return nil, err
	}

	for _, n := range zNodes {
		c := &ConnectionConfig{}

		crtPath := CleanPath(TargetPath) + CleanPath(n)
		data, err := z.getNodeValue(crtPath)
		if err != nil {
			return nil, err
		}

		if err := yaml.Unmarshal(data, c); err != nil {
			log.Error().Err(err).Msgf("Error unmarshalling zNode into yaml: '%s'", crtPath)
			continue
		}

		if err := ValidateConnection(c); err != nil {
			log.Error().Err(err).Msgf("Invalid connection configuration at path: '%s'", crtPath)
			continue
		}

		connectionMap[n] = *c
	}

	zNodes, _, err = z.conn.Children(CleanPath(RequestPath))
	if err != nil {
		return nil, err
	}

	for _, n := range zNodes {
		r := &RequestConfig{}
		crtPath := CleanPath(RequestPath) + CleanPath(n)
		data, err := z.getNodeValue(crtPath)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, r); err != nil {
			log.Error().Err(err).Msgf("Invalid request yaml configuration at path: '%s'", crtPath)
			continue
		}
		requestMap[n] = *r
	}

	targetConf := &TargetConfig{
		Connection: connectionMap,
		Request:    requestMap,
	}

	return targetConf, nil
}

// TODO implement more options for zookeeper client connection
func NewZookeeperClient(hosts []string, timeout time.Duration) (*ZookeeperClient, error) {
	DefaultACLPerms := zk.PermAll
	z := &ZookeeperClient{
		acl: zk.WorldACL(int32(DefaultACLPerms)),
	}
	zkconn, err := z.ConnectToZookeeper(hosts, timeout)
	if err != nil {
		return nil, err
	}
	z.conn = zkconn

	return z, nil
}

func (z *ZookeeperClient) getNodeValue(nodePath string) ([]byte, error) {
	data, _, err := z.conn.Get(nodePath)
	if err != nil {
		log.Log.Error(err, "zNode get failed", "path", nodePath)
		return nil, err
	}
	return data, nil
}

func CleanPath(path string) string {
	return "/" + strings.Trim(path, "/")
}

func (z *ZookeeperClient) ConnectToZookeeper(hosts []string, timeout time.Duration) (*zk.Conn, error) {
	newConn, eventChan, err := zk.Connect(hosts, timeout)
	if err != nil {
		return nil, err
	}
	go z.zookeeperEventHandler(eventChan)
	return newConn, nil
}

func (z *ZookeeperClient) zookeeperEventHandler(zkEventChan <-chan zk.Event) {
	var disconnectedCount int
	for event := range zkEventChan {
		switch event.State {
		case zk.StateDisconnected:
			disconnectedCount++
			log.Log.Info("[ZK] Disconnected. Reconnect attempts:  " + strconv.Itoa(disconnectedCount))
		case zk.StateHasSession:
			disconnectedCount = 0
			log.Log.Info("[ZK] Reconnected to existing session")
		}
	}
}

func CreateFullPath(conn *zk.Conn, path string, acl []zk.ACL) error {
	_, err := conn.Create(path, []byte{}, 0, acl)

	if err == zk.ErrNoNode {
		// Create parent node.
		err = CreateParentPath(conn, path, acl)
		if err != nil {
			return fmt.Errorf("unable to create parent path for path '%s': %v", path, err)
		}

		// Re attempt to add a sequence now that the parent exists
		_, err = conn.Create(path, []byte{}, zk.FlagEphemeral, acl)
		if err != nil {
			return fmt.Errorf("unable to create node after parent path created: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("unable to create path: %v", err)
	}
	return nil
}

func CreateParentPath(conn *zk.Conn, path string, acl []zk.ACL) error {
	parts := strings.Split(path, "/")
	return createPath(conn, parts[:len(parts)-1], acl)
}

func CreatePath(conn *zk.Conn, path string, acl []zk.ACL) error {
	parts := strings.Split(path, "/")
	return createPath(conn, parts, acl)
}

func createPath(conn *zk.Conn, parts []string, acl []zk.ACL) error {
	pth := ""
	for _, p := range parts {
		if p == "" {
			continue
		}

		var exists bool
		pth += "/" + p
		exists, _, err := conn.Exists(pth)
		if err != nil {
			return err
		}
		if exists == true {
			continue
		}
		_, err = conn.Create(pth, []byte{}, 0, acl)
		if err != nil && err != zk.ErrNodeExists {
			return err
		}
	}
	return nil
}

func GetLeafName(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func ValidateConnection(c *ConnectionConfig) error {
	if c == nil {
		return fmt.Errorf("missing target configuration")
	}
	if len(c.Addresses) == 0 {
		return fmt.Errorf("target missing address")
	}
	if c.Request == "" {
		return fmt.Errorf("target missing request")
	}

	return nil
}
