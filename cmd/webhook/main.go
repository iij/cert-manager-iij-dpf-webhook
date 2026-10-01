// SPDX-License-Identifier: Apache-2.0

// Command webhook は cert-manager の DNS01 solver を APIService として起動する。
package main

import (
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
)

// groupName は solver の API グループ (FR-001)。
const groupName = "acme.dns-platform.jp"

func main() {
	cmd.RunWebhookServer(groupName)
}
