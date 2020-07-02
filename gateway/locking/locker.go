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

// DistributedLocker is an interface for creating non-blocking locks among distributed processes.
type DistributedLocker interface {
	// Try to acquire the lock. If the lock is already acquired return true and a deadlock error.
	Try() (bool, error)
	// Unlock the lock.
	Unlock() error
	// Return the ID for this lock.
	ID() string
	// Get the member that currently has the lock for the ID, if it's currently locked, otherwise return an
	// empty string.
	GetMember(id string) (string, error)
}
