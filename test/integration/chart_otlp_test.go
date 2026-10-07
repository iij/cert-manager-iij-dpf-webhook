// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"strings"
	"testing"
)

// containerEnv は Deployment の最初のコンテナの環境変数を返す。
func containerEnv(t *testing.T, docs []map[string]any) map[string]string {
	t.Helper()
	deps := byKind(docs, "Deployment")
	if len(deps) != 1 {
		t.Fatalf("Deployments = %d", len(deps))
	}
	containers, _ := get(deps[0], "spec", "template", "spec", "containers").([]any)
	env := map[string]string{}
	list, _ := get(containers[0], "env").([]any)
	for _, e := range list {
		name, _ := get(e, "name").(string)
		value, _ := get(e, "value").(string)
		env[name] = value
	}
	return env
}

// 原則 V / VI: OTLP 送出は既定で無効。送出先を与えた場合のみ OTEL_EXPORTER_OTLP_* を渡す。
func TestChartOTLP(t *testing.T) {
	docs, err := renderChart(t, "lockOwner=cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	for k := range containerEnv(t, docs) {
		if strings.HasPrefix(k, "OTEL_") {
			t.Errorf("OTLP is configured by default: %s", k)
		}
	}

	docs, err = renderChart(t, "lockOwner=cluster-a", "otlp.endpoint=https://otel.example:4317", "otlp.protocol=grpc")
	if err != nil {
		t.Fatal(err)
	}
	env := containerEnv(t, docs)
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "https://otel.example:4317" || env["OTEL_EXPORTER_OTLP_PROTOCOL"] != "grpc" {
		t.Errorf("env = %v", env)
	}
}
