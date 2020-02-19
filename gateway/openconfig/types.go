// Copyright 2020 Netflix Inc
// Author: Colin McIntosh (colin@netflix.com)

package openconfig

import (
	"github.com/openconfig/goyang/pkg/yang"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type TypeLookup struct {
	modelDirectory string
	treeRoot       map[string]*yang.Entry
}

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
