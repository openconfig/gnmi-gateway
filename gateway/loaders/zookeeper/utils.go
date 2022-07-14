package zookeeper

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/openconfig/gnmi-gateway/gateway/connections"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ZookeeperClient struct {
	acl  []zk.ACL
	conn *zk.Conn
}

func (z *ZookeeperClient) refreshZookeeperConfig(log zerolog.Logger, targetChan chan<- *connections.TargetConnectionControl, config *configuration.GatewayConfig) error {
	if exists, _, _ := z.conn.Exists(GlobalCertPath); exists {
		tlsData, err := z.getNodeValue(GlobalCertPath)
		if err != nil {
			return err
		}
		certConf := &CertificateConfig{}
		if err := yaml.Unmarshal(tlsData, certConf); err != nil {
			return err
		}

		tlsConfig := &tls.Config{
			Renegotiation: tls.RenegotiateNever,
		}

		if len(certConf.ClientCA) == 0 {
			tlsConfig.InsecureSkipVerify = true
		} else {
			certPool := x509.NewCertPool()
			if ok := certPool.AppendCertsFromPEM([]byte(certConf.ClientCA)); !ok {
				return errors.New("invalid ClientCA certificate")
			}
			tlsConfig.RootCAs = certPool
		}

		if len(certConf.ClientCert) > 0 && len(certConf.ClientKey) > 0 {
			cert, err := tls.X509KeyPair([]byte(certConf.ClientCert), []byte(certConf.ClientKey))
			if err != nil {
				return err
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		config.ClientTLSConfig = tlsConfig

		config.Log.Info().Msg("Gateway TLS Config set. Using TLS client authentication")

		controlMsg := new(connections.TargetConnectionControl)
		controlMsg.ReconnectAll = true
		targetChan <- controlMsg
	} else {
		config.Log.Info().Msg("No TLS Certificates found")
	}

	return nil
}

func (z *ZookeeperClient) GetZookeeperData(log zerolog.Logger) (*TargetConfig, error) {
	connectionMap := make(map[string]ConnectionConfig)
	requestMap := make(map[string]RequestConfig)

	if exists, _, _ := z.conn.Exists(CleanPath(TargetPath)); exists {
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
	} else {
		if err := CreateFullPath(z.conn, TargetPath, zk.WorldACL(zk.PermAll)); err != nil {
			return nil, err
		}
	}

	if exists, _, _ := z.conn.Exists(CleanPath(RequestPath)); exists {
		zNodes, _, err := z.conn.Children(CleanPath(RequestPath))
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
	} else {
		if err := CreateFullPath(z.conn, RequestPath, zk.WorldACL(zk.PermAll)); err != nil {
			return nil, err
		}
	}

	targetConf := &TargetConfig{
		Connection: connectionMap,
		Request:    requestMap,
	}

	return targetConf, nil
}

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
		if exists {
			continue
		}
		_, err = conn.Create(pth, []byte{}, 0, acl)
		if err != nil && err != zk.ErrNodeExists {
			return err
		}
	}
	return nil
}

func ValidateConnection(c *ConnectionConfig) error {
	if c == nil {
		return fmt.Errorf("missing target configuration")
	}
	if len(c.Addresses) == 0 {
		return fmt.Errorf("target missing address")
	}

	return nil
}

func (z *ZookeeperClient) AddZookeeperData(t *TargetConfig) error {

	for name, request := range t.Request {
		requestPath := CleanPath(RequestPath) + CleanPath(name)

		if err := z.createNode(requestPath); err != nil {
			return err
		}

		var crtVersion int32

		exists, stat, err := z.conn.Exists(requestPath)
		if err != nil {
			return err
		}
		if exists {
			crtVersion = stat.Version
		} else {
			crtVersion = 0
		}

		requestYaml, err := yaml.Marshal(request)
		if err != nil {
			return err
		}

		if err := z.setNodeValue(requestPath, requestYaml, int32(crtVersion)); err != nil {
			return err
		}
	}

	for name, target := range t.Connection {
		targetPath := CleanPath(TargetPath) + CleanPath(name)

		if err := z.createNode(targetPath); err != nil {
			return err
		}

		var crtVersion int32

		exists, stat, err := z.conn.Exists(targetPath)
		if err != nil {
			return err
		}
		if exists {
			crtVersion = stat.Version
		} else {
			crtVersion = 0
		}

		targetYaml, err := yaml.Marshal(target)
		if err != nil {
			return err
		}

		if err := z.setNodeValue(targetPath, targetYaml, int32(crtVersion)); err != nil {
			return err
		}
	}

	return nil
}

func (z *ZookeeperClient) setNodeValue(nodePath string, value []byte, version int32) error {
	if _, err := z.conn.Set(nodePath, value, version); err != nil {
		log.Log.Error(err, "Node ", nodePath, " set failed")
		return err
	}
	return nil
}

func (z *ZookeeperClient) createNode(path string) error {
	nodePath := CleanPath(path)
	_, err := z.conn.Create(nodePath, []byte{}, 0, z.acl)
	switch err {
	case zk.ErrNoNode:
		err = CreateParentPath(z.conn, nodePath, z.acl)
		if err != nil {
			return fmt.Errorf("unable to create parent path for cluster member registration: %v", err)
		}

		// Re attempt to add a sequence now that the parent exists
		_, err = z.conn.Create(nodePath, []byte{}, 0, z.acl)
		if err != nil {
			return fmt.Errorf("unable to create registration node after parent path created: %v", err)
		}
	case zk.ErrNodeExists:
		log.Log.Info("Node ", nodePath, " found")
	case nil:
		log.Log.Info("Node ", nodePath, " created successfully")
	default:
		log.Log.Error(err, "unable to register node ", nodePath, " with Zookeeper")
		return err
	}

	return nil
}

func (z *ZookeeperClient) Delete(path string) error {
	if exists, stat, _ := z.conn.Exists(CleanPath(path)); exists {
		if err := z.conn.Delete(CleanPath(path), stat.Version); err != nil {
			return err
		}
	}

	return nil
}
