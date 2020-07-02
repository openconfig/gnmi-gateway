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

package locking

import (
	"fmt"
	"sync"
)

// Registry fulfills the distributed portion of the DistributedLocker interface but only for the current process. That
// is to say, this implementation of DistributedLocker isn't actually distributed.
var registry sync.Map

// NonBlockingLock is an implementation of DistributedLocker that does NOT support multiple processes. NonBlockingLock
// is used for testing and documenting a reference implementation of DistributedLocker. Do not use this in production
// unless you have a clear understanding of how this works.
type NonBlockingLock struct {
	acquired bool
	id       string
	member   string
}

func NewNonBlockingLock(id string, member string) DistributedLocker {
	return &NonBlockingLock{
		id:     id,
		member: member,
	}
}

func (l *NonBlockingLock) GetMember(id string) (string, error) {
	val, exists := registry.Load(id)
	if exists {
		return val.(string), nil
	}
	return "", nil
}

func (l *NonBlockingLock) ID() string {
	return l.id
}

func (l *NonBlockingLock) Try() (bool, error) {
	if l.acquired {
		return true, fmt.Errorf("deadlock: lock for id '%s' is already acquired", l.id)
	}
	_, exists := registry.LoadOrStore(l.id, l.member)
	if exists {
		// lock was already taken
		return false, nil
	}
	l.acquired = true
	return true, nil
}

func (l *NonBlockingLock) Unlock() error {
	if !l.acquired {
		return fmt.Errorf("unable to unlock: lock is not yet acquired for id '%s'", l.id)
	}
	registry.Delete(l.id)
	l.acquired = false
	return nil
}
