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

package targets

import targetpb "github.com/openconfig/gnmi/proto/target"

type TargetLoader interface {
	// Get the Configuration once.
	GetConfiguration() (*targetpb.Configuration, error)
	// Start the loader, if necessary.
	Start() error
	// Start watching the configuration for changes and send the entire Configuration to the supplied channel when
	// a change is detected.
	WatchConfiguration(chan *targetpb.Configuration)
}
