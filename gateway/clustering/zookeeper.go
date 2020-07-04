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
	member         string
}

func NewZookeeperCluster(config *configuration.GatewayConfig, conn *zk.Conn, member string) *ZookeeperCluster {
	return &ZookeeperCluster{
		acl:    zk.WorldACL(DefaultACLPerms),
		config: config,
		conn:   conn,
		member: member,
	}
}

func (z *ZookeeperCluster) Member() string {
	return z.member
}

// Register will create an ephemeral Zookeeper node in a cluster member tree. The node
// will be removed automatically by Zookeeper if the session disconnects.
func (z *ZookeeperCluster) Register() error {
	registrationPath := CleanPath(z.config.ZookeeperPrefix) + CleanPath(ClusterMemberPath) + CleanPath(z.member)

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
	searchPath := CleanPath(z.config.ZookeeperPrefix) + CleanPath(ClusterMemberPath)

	if z.callbackCancel != nil {
		z.callbackCancel()
	}

	var stopped bool
	go func() {
		// TODO (cmcintosh): This whole goroutine has awful error handling and needs to be rewritten.
		var previousMembers []string
		var consecutiveErrors = 0
		for !stopped {
			currentMembers, _, memberListChanges, err := z.conn.ChildrenW(searchPath)

			//if err == zk.ErrNoNode {
			//	createErr := CreatePath(z.conn, searchPath, z.acl)
			//	if createErr != nil {
			//		err
			//		z.config.Log.Error().Msgf()
			//	} else
			//}

			if err != nil {
				consecutiveErrors++
				z.config.Log.Error().Msgf("error #%d while trying to set Zookeeper watch on cluster member tree: %v", consecutiveErrors, err)
				if consecutiveErrors >= 3 {
					panic(fmt.Errorf("too many consecutive errors trying to watch the cluster member tree"))
				}
				// Sleep to avoid retrying too quickly
				// 2*ZookeeperTimeout should be plenty of time if the underlying connection is the issue
				time.Sleep(2 * z.config.ZookeeperTimeout)
				continue
			}

			consecutiveErrors = 0

			for _, currentMember := range currentMembers {
				if currentMember == z.member {
					continue
				}

				if !stringInSlice(currentMember, previousMembers) {
					callback(currentMember, "") // add
				}
			}
			for _, previousMember := range previousMembers {
				if previousMember == z.member {
					continue
				}

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

	searchPath := CleanPath(z.config.ZookeeperPrefix) + CleanPath(ClusterMemberPath)
	children, _, err := z.conn.Children(searchPath)
	if err != nil {
		return nil, fmt.Errorf("error while trying to set Zookeeper watch on cluster member tree: %v", err)
	}

	for _, member := range children {
		if member != "" && member != z.member {
			clusterMembers = append(clusterMembers, member)
		}
	}
	return clusterMembers, nil
}

func CreateParentPath(conn *zk.Conn, path string, acl []zk.ACL) error {
	parts := strings.Split(path, "/")
	return createPath(conn, parts[:len(parts)-1], acl)
}

//func CreatePath(conn *zk.Conn, path string, acl []zk.ACL) error {
//	parts := strings.Split(path, "/")
//	return createPath(conn, parts, acl)
//}

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

func CleanPath(path string) string {
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
