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

package simple

import (
	"github.com/openconfig/gnmi-gateway/gateway/configuration"
	"github.com/stretchr/testify/assert"
	"testing"
)

const TestData = `
---
connection:
  my-router:
    addresses:
      - my-router.test.example.net:9339
    request: my-request
    meta: {}
request:
  my-request:
    target: "*"
    paths:
      - /components
      - /interfaces/interface[name=*]/state/counters
      - /qos/interfaces/interface[interface-id=*]/output/queues/queue[name=*]/state
`

func TestSimpleTargetLoader_yamlToTargets(t *testing.T) {
	assertion := assert.New(t)

	loader := &SimpleTargetLoader{
		config: &configuration.GatewayConfig{},
	}
	data := []byte(TestData)
	targetConfig, err := loader.yamlToTargets(&data)
	assertion.NoError(err)
	assertion.NotNil(targetConfig)
	assertion.NotNil(targetConfig.Request["my-request"])
	assertion.Equal("*", targetConfig.Request["my-request"].GetSubscribe().GetPrefix().Target)
	assertion.Len(targetConfig.Request["my-request"].GetSubscribe().GetSubscription(), 3)
	assertion.NotNil(targetConfig.Target["my-router"])
	assertion.Len(targetConfig.Target["my-router"].GetAddresses(), 1)
	assertion.Contains(targetConfig.Target["my-router"].GetAddresses(), "my-router.test.example.net:9339")
	assertion.Equal("my-request", targetConfig.Target["my-router"].GetRequest())
}
