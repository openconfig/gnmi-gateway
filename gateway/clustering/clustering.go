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

// Package clustering provides an interface to register and list members in a cluster.
package clustering

// MemberID is a string identifier that is unique for each cluster member.
type MemberID string

// MemberListCallbackFunc is a function that will be called once for every member added
// or removed from the cluster.
type MemberListCallbackFunc func(add MemberID, remove MemberID)

// ClusterMember is one member of a cluster.
type ClusterMember interface {
	// MemberID returns the unique identifier for this cluster member.
	MemberID() MemberID
	// MemberList returns a current list of the cluster members. Returns an error
	// if the list was unable to be retrieved.
	MemberList() ([]MemberID, error)
	// MemberListCallback will call the callback function once for every member
	// currently in the tree and then again for each future addition and removal.
	// Subsequent calls to MemberListCallback will cancel previous callbacks.
	MemberListCallback(callback MemberListCallbackFunc) error
	// Register will register this member with the cluster. Once a cluster member is
	// registered it should stay registered as long as the cluster member is alive.
	// If registration was unsuccessful an error is returned.
	Register() error
	// Unregister will unregister this member with the cluster. Unregister may not be
	// called so arrangements should be made for the cluster to unregister a
	// member if it dies unexpectedly.
	Unregister() error
}
