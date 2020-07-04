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

package clustering

import (
	"fmt"
	"github.com/samuel/go-zookeeper/zk"
	"stash.corp.netflix.com/ocnas/gnmi-gateway/gateway/configuration"
	"strings"
	"time"
)

const ClusterMemberPath = "/members"
const DefaultACLPerms = zk.PermAll

type ZookeeperCluster struct {
	acl            []zk.ACL
	config         *configuration.GatewayConfig
	conn           *zk.Conn
	callbackCancel func()
}

func NewZookeeperCluster(config *configuration.GatewayConfig, conn *zk.Conn) *ZookeeperCluster {
	return &ZookeeperCluster{
		acl:    zk.WorldACL(DefaultACLPerms),
		config: config,
		conn:   conn,
	}
}

// Register will create an ephemeral Zookeeper node in a cluster member tree. The node
// will be removed automatically by Zookeeper if the session disconnects.
func (z *ZookeeperCluster) Register(member string) error {
	registrationPath := cleanPath(z.config.ZookeeperPrefix) + cleanPath(ClusterMemberPath) + cleanPath(member)

	_, err := z.conn.Create(registrationPath, []byte{}, zk.FlagEphemeral, z.acl)

	if err == zk.ErrNoNode {
		// Create parent node.
		err = CreateParentPath(z.conn, registrationPath, z.acl)
		if err != nil {
			return fmt.Errorf("unable to create parent path for cluster member registration: %v", err)
		}

		// Re attempt to add a sequence now that the parent exists
		_, err = z.conn.Create(registrationPath, []byte{}, zk.FlagEphemeral, z.acl)
		if err != nil {
			return fmt.Errorf("unable to create registration node after parent path created: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("unable to register cluster member with Zookeeper: %v", err)
	}
	return nil
}

type MemberChangeCallback func(add string, remove string)

// MemberChangeCallback will call the callback function once for every member currently in the tree and then
// again for each future addition and removal. Subsequent calls to MemberChangeCallback will cancel previous
// callbacks.
func (z *ZookeeperCluster) MemberChangeCallback(callback MemberChangeCallback) error {
	searchPath := cleanPath(z.config.ZookeeperPrefix) + cleanPath(ClusterMemberPath)

	if z.callbackCancel != nil {
		z.callbackCancel()
	}

	var stopped bool
	go func() {
		var previousMembers []string
		var consecutiveErrors = 0
		for !stopped {
			currentMembers, _, memberListChanges, err := z.conn.ChildrenW(searchPath)
			if err != nil {
				consecutiveErrors++
				z.config.Log.Error().Msgf("error #%d while trying to set Zookeeper watch on cluster member tree: %v", consecutiveErrors, err)
				if consecutiveErrors >= 3 {
					panic(fmt.Errorf("too many consecutive errors trying to watch the cluster member tree"))
				}
				// Sleep to avoid retrying too quickly
				// 2*ZookeeperTimeout should be plenty of time if the underlying connection is the issue
				time.Sleep(2 * z.config.ZookeeperTimeout)
			}

			for _, currentMember := range currentMembers {
				if !stringInSlice(currentMember, previousMembers) {
					callback(currentMember, "") // add
				}
			}
			for _, previousMember := range previousMembers {
				if !stringInSlice(previousMember, currentMembers) {
					callback("", previousMember) // remove
				}
			}
			previousMembers = currentMembers

			<-memberListChanges
		}
	}()

	z.callbackCancel = func() {
		stopped = true
	}
	return nil
}

func (z *ZookeeperCluster) MemberList() ([]string, error) {
	var clusterMembers []string

	searchPath := cleanPath(z.config.ZookeeperPrefix) + cleanPath(ClusterMemberPath)
	children, _, err := z.conn.Children(searchPath)
	if err != nil {
		return nil, fmt.Errorf("error while trying to set Zookeeper watch on cluster member tree")
	}

	for _, member := range children {
		if member != "" {
			clusterMembers = append(clusterMembers, member)
		}
	}
	return clusterMembers, nil
}

func CreateParentPath(conn *zk.Conn, path string, acl []zk.ACL) error {
	parts := strings.Split(path, "/")
	pth := ""
	for _, p := range parts[:len(parts)-1] {
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

func cleanPath(path string) string {
	return "/" + strings.Trim(path, "/")
}

func stringInSlice(s string, list []string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
