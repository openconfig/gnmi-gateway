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

package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/mspiez/gnmi-gateway/gateway"
	"github.com/mspiez/nautobot-go-client/nautobot"
)

func main() {
	gateway.Main()
	token := os.Getenv("NAUTOBOT_TOKEN")
	baseUrl := os.Getenv("NAUTOBOT_URL")

	// optionalFields := nautobot.OptionalFilter{"slug__ic", "site"}

	ctx := context.Background()
	timeout := 30 * time.Second
	reqContext, _ := context.WithTimeout(ctx, timeout)

	newClient := nautobot.NewClient(
		nautobot.WithToken(token),
		nautobot.WithBaseURL(baseUrl),
		nautobot.WithOffset(-1),
		// nautobot.WithRetries(1),
	)
	err := newClient.DeleteInterfaceStatus(reqContext, "r2__ethernet4")

	if err != nil {
		log.Fatal(err)
	}
}
