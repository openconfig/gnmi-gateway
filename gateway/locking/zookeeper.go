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

// Copyright (c) 2013, Samuel Stauffer <samuel@descolada.com>
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// * Redistributions of source code must retain the above copyright
//   notice, this list of conditions and the following disclaimer.
// * Redistributions in binary form must reproduce the above copyright
//   notice, this list of conditions and the following disclaimer in the
//   documentation and/or other materials provided with the distribution.
// * Neither the name of the author nor the
//   names of its contributors may be used to endorse or promote products
//   derived from this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
// WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL <COPYRIGHT HOLDER> BE LIABLE FOR ANY
// DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
// (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
// LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
// ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
// SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// Portions of this file including parseSeq() and try() (excluding modifications) are from
// https://github.com/samuel/go-zookeeper/blob/2cc03de413da42869e2db7ce7965d3e978d917eb/zk/lock.go

package locking

import (
	"fmt"
	"github.com/go-zookeeper/zk"
	"strconv"
	"strings"
	"time"
)

type ZookeeperNonBlockingLock struct {
	acquired bool
	conn     *zk.Conn
	// The member that is holding the lock. This is usually the address and port where the cluster member is reachable.
	member   string
	id       string
	acl      []zk.ACL
	lockPath string
	seq      int
}

// NewZookeeperNonBlockingLock creates a new lock instance using the provided connection, path, and acl.
// The path must be a node that is only used by this lock. A lock instances starts
// unlocked until Try() is called.
func NewZookeeperNonBlockingLock(conn *zk.Conn, id string, member string, acl []zk.ACL) DistributedLocker {
	trimmedID := "/" + strings.Trim(id, "/")
	return &ZookeeperNonBlockingLock{
		conn:   conn,
		member: member,
		id:     trimmedID,
		acl:    acl,
	}
}

func GetMember(conn *zk.Conn, id string) (string, error) {
	trimmedID := "/" + strings.Trim(id, "/")
	_, lowestSeqPath, err := lowestSeqChild(conn, trimmedID)
	if err != nil {
		return "", fmt.Errorf("unable to find lowest sequence path: %s", err)
	}
	if lowestSeqPath == "" {
		return "", nil
	}
	data, _, err := conn.Get(lowestSeqPath)
	if err != nil {
		return "", fmt.Errorf("unable to retrieve lowest sequence child: %s", err)
	}
	return string(data), nil
}

func (l *ZookeeperNonBlockingLock) GetMember(id string) (string, error) {
	return GetMember(l.conn, id)
}

func (l *ZookeeperNonBlockingLock) ID() string {
	return l.id
}

func parseSeq(path string) (int, error) {
	parts := strings.Split(path, "-")
	return strconv.Atoi(parts[len(parts)-1])
}

func (l *ZookeeperNonBlockingLock) Try() (bool, error) {
	var err error
	currentState := l.conn.State()
	if currentState == zk.StateConnected || currentState == zk.StateHasSession {
		l.acquired, err = l.try()
		if l.acquired {
			go l.watchState()
		}
		return l.acquired, err
	}
	return false, fmt.Errorf("not connected to Zookeeper")
}

// Lock attempts to acquire the lock. It will return false if the lock
// is not acquired or an error occurs. If this instance already has the lock
// then ErrDeadlock is returned.
func (l *ZookeeperNonBlockingLock) try() (bool, error) {
	if l.lockPath != "" {
		return true, zk.ErrDeadlock
	}

	prefix := fmt.Sprintf("%s/lock-", l.id)

	// Attempt to add a sequence to the tree
	path, err := l.conn.CreateProtectedEphemeralSequential(prefix, []byte(l.member), l.acl)

	if err == zk.ErrNoNode {
		// Create parent node.
		err = l.createParentPath(prefix)
		if err != nil {
			return false, fmt.Errorf("unable to create parent node path: %v", err)
		}

		// Re attempt to add a sequence now that the parent exists
		path, err = l.conn.CreateProtectedEphemeralSequential(prefix, []byte(l.member), l.acl)
		if err != nil {
			return false, fmt.Errorf("unable to create node after parent path created: %v", err)
		}
	} else if err != nil {
		return false, fmt.Errorf("unable to add sequence to tree: %v", err)
	}

	seq, err := parseSeq(path)
	if err != nil {
		return false, err
	}

	lowestSeq, _, err := l.lowestSeqChild(l.id)
	if err != nil {
		return false, err
	}

	if seq != lowestSeq {
		// Did not acquire the lock
		if err := l.conn.Delete(path, -1); err != nil {
			return false, fmt.Errorf("unable to remove unacquired lock cleanly: %s", err)
		}
		return false, nil
	}
	// Acquired the lock
	l.seq = seq
	l.lockPath = path
	return true, nil
}

func (l *ZookeeperNonBlockingLock) createParentPath(path string) error {
	parts := strings.Split(path, "/")
	pth := ""
	for _, p := range parts[:len(parts)-1] {
		if p == "" {
			continue
		}

		var exists bool
		pth += "/" + p
		exists, _, err := l.conn.Exists(pth)
		if err != nil {
			return err
		}
		if exists == true {
			continue
		}
		_, err = l.conn.Create(pth, []byte{}, 0, l.acl)
		if err != nil && err != zk.ErrNodeExists {
			return err
		}
	}
	return nil
}

func lowestSeqChild(conn *zk.Conn, path string) (int, string, error) {
	children, _, err := conn.Children(path)
	if err != nil {
		return 0, "", err
	}

	lowestSeq := -1
	lowestSeqPath := ""
	for _, childPath := range children {
		childSeq, err := parseSeq(childPath)
		if err != nil {
			return 0, "", err
		}
		if lowestSeq == -1 || childSeq < lowestSeq {
			lowestSeq = childSeq
			lowestSeqPath = path + "/" + childPath
		}
	}
	return lowestSeq, lowestSeqPath, nil
}

func (l *ZookeeperNonBlockingLock) lowestSeqChild(path string) (int, string, error) {
	return lowestSeqChild(l.conn, path)
}

func (l *ZookeeperNonBlockingLock) watchState() {
	currentState := l.conn.State()
	for l.acquired && (currentState == zk.StateConnected || currentState == zk.StateHasSession) {
		time.Sleep(500 * time.Millisecond)
		currentState = l.conn.State()
	}
	// disconnected
	if l.acquired {
		l.released()
	}
}

// Unlock releases an acquired lock. If the lock is not currently acquired by
// this Lock instance than ErrNotLocked is returned.
// This should only be called if we're still connected.
func (l *ZookeeperNonBlockingLock) Unlock() error {
	if l.lockPath == "" {
		return zk.ErrNotLocked
	}
	if err := l.conn.Delete(l.lockPath, -1); err != nil {
		return fmt.Errorf("unable to release lock gracefully: %s", err)
	}
	l.released()
	//log.Info().Msg("Cluster lock released.")
	return nil
}

func (l *ZookeeperNonBlockingLock) released() {
	l.lockPath = ""
	l.seq = 0
	l.acquired = false
}
