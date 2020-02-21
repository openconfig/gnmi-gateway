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
	"golang.org/x/sync/semaphore"
	"sync"
)

type MappedLocker interface {
	Get(string) NonBlockingLocker
}

func NewLocalMappedLocker() MappedLocker {
	return &MappedLock{
		lookup: make(map[string]NonBlockingLocker),
	}
}

type MappedLock struct {
	lookup map[string]NonBlockingLocker
	mutex  sync.Mutex
}

func (m *MappedLock) Get(name string) NonBlockingLocker {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	lock, exists := m.lookup[name]
	if !exists {
		lock = NewNonBlockingLock()
		m.lookup[name] = lock
	}
	return lock
}

type NonBlockingLocker interface {
	Try() bool
	Unlock()
}

type NonBlockingLock struct {
	sem *semaphore.Weighted
}

func NewNonBlockingLock() NonBlockingLocker {
	return &NonBlockingLock{
		sem: semaphore.NewWeighted(1),
	}
}

func (l *NonBlockingLock) Try() bool {
	return l.sem.TryAcquire(1)
}

func (l *NonBlockingLock) Unlock() {
	l.sem.Release(1)
}
