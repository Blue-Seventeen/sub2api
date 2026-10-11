package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestProxyNodeStatusJSONSeparatesNodeFromRuntime(t *testing.T) {
	var proxy service.Proxy
	if err := json.Unmarshal([]byte(`{"ID":3,"NodeStatus":"source_missing","RuntimeStatus":{"enabled":true,"status":"running"}}`), &proxy); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{
		"proxy": ProxyFromService(&proxy), "admin": ProxyFromServiceAdmin(&proxy),
		"counted":       ProxyWithAccountCountFromService(&service.ProxyWithAccountCount{Proxy: proxy}),
		"admin counted": ProxyWithAccountCountFromServiceAdmin(&service.ProxyWithAccountCount{Proxy: proxy}),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var out map[string]any
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatal(err)
			}
			if out["node_status"] != "source_missing" {
				t.Error("missing node state was omitted from proxy JSON")
			}
			runtime, ok := out["runtime_status"].(map[string]any)
			if !ok || runtime["status"] != "running" {
				t.Error("node state replaced subscription runtime state")
			}
		})
	}
}
func TestProxyNodeStatusJSONOmittedForManualProxy(t *testing.T) {
	data, err := json.Marshal(ProxyFromServiceAdmin(&service.Proxy{ID: 4, SourceType: service.ProxySourceManual}))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["node_status"]; ok {
		t.Error("manual proxy emitted an empty node state")
	}
}
