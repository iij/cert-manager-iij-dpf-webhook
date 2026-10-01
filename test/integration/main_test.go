// SPDX-License-Identifier: Apache-2.0

// Package integration は模擬 DPF に対して solver を通しで動かす結合テストである。
package integration

import (
	"os"
	"testing"

	sdk "github.com/iij/dpf-go"
)

func TestMain(m *testing.M) {
	sdk.SyncWaitPollInterval = 0
	os.Exit(m.Run())
}
