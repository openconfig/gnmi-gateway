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

// Copyright 2015 Google Inc.
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

// Portions of this file including LoadAllModules (excluding modifications) are from
// https://github.com/openconfig/goyang/blob/6be32aef2bcd1a13b8d2d959bfc9d50673c0f57d/yang.go

// Package openconfig provides useful types and functions for interacting
// with OpenConfig modeled data.
package openconfig

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openconfig/goyang/pkg/yang"
)

type TypeLookup struct {
	modelDirectory string
	treeRoot       map[string]*yang.Entry
}

// GetTypeByPath returns the YANG type string for the given OpenConfig path.
func (t *TypeLookup) GetTypeByPath(path []string) string {
	entry, exists := t.treeRoot[path[0]]
	if exists {
		return getTypeByPath(entry, path[1:])
	}
	return ""
}

func getTypeByPath(entry *yang.Entry, path []string) string {
	child, exists := entry.Dir[path[0]]
	if exists {
		if len(path) == 1 {
			return child.Type.Name
		}
		return getTypeByPath(entry.Dir[path[0]], path[1:])
	} else {
		return ""
	}
}

// GetTypeByPath returns the YANG type string for the given OpenConfig path.
// Portions of this are originally from github.com/openconfig/goyang/yang.go
func (t *TypeLookup) LoadAllModules(searchPath string) error {
	expanded, err := yang.PathsWithModules(searchPath)
	if err != nil {
		return err
	}
	yang.AddPath(expanded...)

	var fileList []string
	err = filepath.Walk(searchPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if strings.HasSuffix(info.Name(), ".yang") {
				fileList = append(fileList, strings.TrimSuffix(info.Name(), ".yang"))
			}
			return nil
		})
	if err != nil {
		return err
	}

	ms := yang.NewModules()

	for _, fileName := range fileList {
		if err := ms.Read(fileName); err != nil {
			return err
		}
	}

	ms.Process()

	mods := map[string]*yang.Module{}
	var names []string

	for _, m := range ms.Modules {
		if mods[m.Name] == nil {
			mods[m.Name] = m
			names = append(names, m.Name)
		}
	}
	sort.Strings(names)
	entries := make(map[string]*yang.Entry, len(names))
	for _, name := range names {
		entries[name] = yang.ToEntry(mods[name])
	}

	// Load all of the OpenConfig root entries. This ignores non-openconfig-prefixed files
	root := make(map[string]*yang.Entry)
	for fileName, fileEntry := range entries {
		if strings.HasPrefix(fileName, "openconfig") {
			for name, rootEntry := range fileEntry.Dir {
				root[name] = rootEntry
			}
		}
	}
	t.treeRoot = root

	return nil
}
