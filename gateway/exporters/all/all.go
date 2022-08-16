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

package all

import (
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/azure"
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/debug"
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/fluentd"
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/influxdb"
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/kafka"
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/prometheus"
	_ "github.com/openconfig/gnmi-gateway/gateway/exporters/statsd"
)
