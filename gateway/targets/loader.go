// Copyright 2020 Netflix Inc
// Author: Colin McIntosh (colin@netflix.com)

package targets

import targetpb "github.com/openconfig/gnmi/proto/target"

type TargetLoader interface {
	// Get the Configuration once.
	GetConfiguration() (*targetpb.Configuration, error)
	// Start watching the configuration for changes and send the entire Configuration to the supplied channel when
	// a change is detected.
	WatchConfiguration(chan *targetpb.Configuration)
}
