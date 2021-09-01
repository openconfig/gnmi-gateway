// +build integration

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

package openconfig

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTypeLookup_GetTypeByPath(t *testing.T) {
	lookup := new(TypeLookup)
	err := lookup.LoadAllModules("../../oc-models/")
	require.NoError(t, err)

	require.Equal(t, "counter64", lookup.GetTypeByPath([]string{"interfaces", "interface", "state", "counters", "out-octets"}))
	require.Empty(t, lookup.GetTypeByPath([]string{"system", "memory", "state"}))
	require.Empty(t, lookup.GetTypeByPath([]string{"blah", "blah", "state"}))
}
