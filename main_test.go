package main

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// sortKeys extracts the sort keys from a catalog body for assertions.
func sortKeys(t *testing.T, body []byte, key string) []string {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(root[key], &items); err != nil {
		t.Fatalf("decode %q list: %v", key, err)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, modelSortKey(item))
	}
	return out
}

// interceptFor runs the response interceptor and returns the decoded result.
func interceptFor(t *testing.T, req pluginapi.ResponseInterceptRequest) pluginapi.ResponseInterceptResponse {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	out, err := interceptResponse(raw)
	if err != nil {
		t.Fatalf("intercept: %v", err)
	}
	var env envelope
	if err = json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok envelope, got %+v", env.Error)
	}
	var resp pluginapi.ResponseInterceptResponse
	if err = json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestSortOpenAICatalog(t *testing.T) {
	body := []byte(`{"object":"list","data":[` +
		`{"id":"zeta","object":"model","owned_by":"x"},` +
		`{"id":"alpha","object":"model","owned_by":"y"},` +
		`{"id":"mid","object":"model"}]}`)

	out, changed := sortModelCatalog(body)
	if !changed {
		t.Fatal("catalog should have been reordered")
	}
	got := sortKeys(t, out, "data")
	want := []string{"alpha", "mid", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	// Top-level siblings and per-entry fields must survive the re-encode.
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if root["object"] != "list" {
		t.Fatalf("object field lost: %v", root["object"])
	}
	first := root["data"].([]any)[0].(map[string]any)
	if first["owned_by"] != "y" {
		t.Fatalf("owned_by lost during re-encode: %v", first)
	}
}

func TestSortGeminiCatalogUsesName(t *testing.T) {
	// The Gemini format has no "id" field; its sort key is "name".
	body := []byte(`{"models":[` +
		`{"name":"models/zeta","displayName":"Z"},` +
		`{"name":"models/alpha","displayName":"A"}]}`)

	out, changed := sortModelCatalog(body)
	if !changed {
		t.Fatal("gemini catalog should have been reordered")
	}
	got := sortKeys(t, out, "models")
	if got[0] != "models/alpha" || got[1] != "models/zeta" {
		t.Fatalf("gemini order = %v", got)
	}
}

func TestAlreadySortedIsNotRewritten(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"a"},{"id":"b"}]}`)
	if _, changed := sortModelCatalog(body); changed {
		t.Fatal("a sorted catalog must not be rewritten")
	}
}

func TestMalformedBodiesAreLeftAlone(t *testing.T) {
	cases := map[string][]byte{
		"empty":            nil,
		"not json":         []byte(`not json`),
		"no list":          []byte(`{"object":"list"}`),
		"single entry":     []byte(`{"data":[{"id":"a"}]}`),
		"entry without id": []byte(`{"data":[{"id":"b"},{"object":"model"}]}`),
		"list of scalars":  []byte(`{"data":["b","a"]}`),
		"upstream error":   []byte(`{"error":{"message":"boom"}}`),
	}
	for name, body := range cases {
		if out, changed := sortModelCatalog(body); changed {
			t.Fatalf("%s: body must stay untouched, got %s", name, out)
		}
	}
}

// The critical property: completions must never be modified, otherwise the
// plugin would corrupt chat responses.
func TestCompletionsAreNotTouched(t *testing.T) {
	completion := []byte(`{"id":"chatcmpl-1","object":"chat.completion",` +
		`"choices":[{"message":{"content":"ok"}}]}`)

	cases := []pluginapi.ResponseInterceptRequest{
		{Model: "claude-opus-4-6", Body: completion},
		{RequestedModel: "anthropic-claude-opus-4-6", Body: completion},
		{Model: "gpt", Stream: true, Body: completion},
	}
	for _, req := range cases {
		if resp := interceptFor(t, req); len(resp.Body) != 0 {
			t.Fatalf("completion for model %q was modified: %s", req.Model, resp.Body)
		}
	}
}

// Listings arrive with empty Model and RequestedModel, so they get sorted.
func TestModelListIsSortedThroughIntercept(t *testing.T) {
	resp := interceptFor(t, pluginapi.ResponseInterceptRequest{
		SourceFormat: "openai",
		Body:         []byte(`{"object":"list","data":[{"id":"zeta"},{"id":"alpha"}]}`),
	})
	if len(resp.Body) == 0 {
		t.Fatal("catalog should have been sorted")
	}
	got := sortKeys(t, resp.Body, "data")
	if got[0] != "alpha" || got[1] != "zeta" {
		t.Fatalf("order = %v", got)
	}
}

func TestRegistrationDeclaresOnlyResponseInterceptor(t *testing.T) {
	reg := pluginRegistration()
	if !reg.Capabilities.ResponseInterceptor {
		t.Fatal("response_interceptor capability must be declared")
	}
	// The host rejects a plugin whose schema version exceeds its own.
	if reg.SchemaVersion == 0 {
		t.Fatal("schema_version must be populated")
	}
	if reg.Metadata.Name != pluginID {
		t.Fatalf("plugin name = %q, want %q", reg.Metadata.Name, pluginID)
	}
}

// validPlugin in the host requires Name, Version, Author AND GitHubRepository.
// Leaving any of them empty makes the plugin load but never register.
func TestRegistrationMetadataIsComplete(t *testing.T) {
	meta := pluginRegistration().Metadata
	required := map[string]string{
		"Name":             meta.Name,
		"Version":          meta.Version,
		"Author":           meta.Author,
		"GitHubRepository": meta.GitHubRepository,
	}
	for field, value := range required {
		if value == "" {
			t.Fatalf("metadata field %s is empty; the host would reject registration", field)
		}
	}
}

func TestUnknownMethodReturnsErrorEnvelope(t *testing.T) {
	out, err := handleMethod("does.not.exist", nil)
	if err != nil {
		t.Fatalf("unknown method must not return a Go error: %v", err)
	}
	var env envelope
	if err = json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "unknown_method" {
		t.Fatalf("expected unknown_method error envelope, got %s", out)
	}
}
