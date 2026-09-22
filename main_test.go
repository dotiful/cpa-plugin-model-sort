package main

import (
	"encoding/json"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// withConfig applies a configuration for one test and restores the previous
// one afterwards, so tests stay independent of execution order.
func withConfig(t *testing.T, s settings) {
	t.Helper()
	previous := config
	config = s
	t.Cleanup(func() { config = previous })
}

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

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
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

// mustLifecycleRequest builds the JSON the host sends, with config_yaml carried
// as a []byte (base64 in JSON) exactly as the host encodes it.
func mustLifecycleRequest(t *testing.T, configYAML string) []byte {
	t.Helper()
	raw, err := json.Marshal(lifecycleRequest{ConfigYAML: []byte(configYAML)})
	if err != nil {
		t.Fatalf("encode lifecycle request: %v", err)
	}
	return raw
}

func TestSortOpenAICatalog(t *testing.T) {
	body := []byte(`{"object":"list","data":[` +
		`{"id":"zeta","object":"model","owned_by":"x"},` +
		`{"id":"alpha","object":"model","owned_by":"y"},` +
		`{"id":"mid","object":"model"}]}`)

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("catalog should have been reordered")
	}
	assertOrder(t, sortKeys(t, out, "data"), []string{"alpha", "mid", "zeta"})

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

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("gemini catalog should have been reordered")
	}
	assertOrder(t, sortKeys(t, out, "models"), []string{"models/alpha", "models/zeta"})
}

func TestAlreadySortedIsNotRewritten(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"a"},{"id":"b"}]}`)
	if _, changed := curateModelCatalog(body); changed {
		t.Fatal("a sorted catalog must not be rewritten")
	}
}

func TestDescendingOrder(t *testing.T) {
	withConfig(t, settings{Order: orderDescending})
	body := []byte(`{"object":"list","data":[{"id":"a"},{"id":"b"},{"id":"c"}]}`)

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("descending order should have reversed the catalog")
	}
	assertOrder(t, sortKeys(t, out, "data"), []string{"c", "b", "a"})
}

func TestPinnedMovesModelsToTopInConfiguredOrder(t *testing.T) {
	withConfig(t, settings{Order: orderAscending, Pinned: []string{"zeta", "mid"}})
	body := []byte(`{"object":"list","data":[{"id":"alpha"},{"id":"mid"},{"id":"zeta"}]}`)

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("pinned models should have been moved to the top")
	}
	// Pins keep their configured order; the rest stays sorted behind them.
	assertOrder(t, sortKeys(t, out, "data"), []string{"zeta", "mid", "alpha"})
}

func TestPinnedIgnoresUnknownModels(t *testing.T) {
	withConfig(t, settings{Order: orderAscending, Pinned: []string{"does-not-exist"}})
	body := []byte(`{"object":"list","data":[{"id":"b"},{"id":"a"}]}`)

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("catalog should still be sorted")
	}
	assertOrder(t, sortKeys(t, out, "data"), []string{"a", "b"})
}

func TestHiddenRemovesModelsFromCatalog(t *testing.T) {
	withConfig(t, settings{Order: orderAscending, Hidden: []string{"secret"}})
	body := []byte(`{"object":"list","data":[{"id":"a"},{"id":"secret"},{"id":"b"}]}`)

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("hidden model should have been removed")
	}
	assertOrder(t, sortKeys(t, out, "data"), []string{"a", "b"})
}

// The Gemini catalog reports "models/<id>", so a bare ID in the configuration
// must still match; otherwise hiding a Gemini model would silently do nothing.
func TestHiddenMatchesGeminiBareID(t *testing.T) {
	withConfig(t, settings{Order: orderAscending, Hidden: []string{"gemini-secret"}})
	body := []byte(`{"models":[{"name":"models/gemini-a"},{"name":"models/gemini-secret"}]}`)

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("hidden gemini model should have been removed")
	}
	assertOrder(t, sortKeys(t, out, "models"), []string{"models/gemini-a"})
}

func TestHidingEveryModelYieldsEmptyCatalog(t *testing.T) {
	withConfig(t, settings{Order: orderAscending, Hidden: []string{"a", "b"}})
	body := []byte(`{"object":"list","data":[{"id":"a"},{"id":"b"}]}`)

	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("catalog should have been emptied")
	}
	if got := sortKeys(t, out, "data"); len(got) != 0 {
		t.Fatalf("catalog = %v, want empty", got)
	}
}

func TestMalformedBodiesAreLeftAlone(t *testing.T) {
	cases := map[string][]byte{
		"empty":            nil,
		"not json":         []byte(`not json`),
		"no list":          []byte(`{"object":"list"}`),
		"empty list":       []byte(`{"data":[]}`),
		"entry without id": []byte(`{"data":[{"id":"b"},{"object":"model"}]}`),
		"list of scalars":  []byte(`{"data":["b","a"]}`),
		"upstream error":   []byte(`{"error":{"message":"boom"}}`),
	}
	for name, body := range cases {
		if out, changed := curateModelCatalog(body); changed {
			t.Fatalf("%s: body must stay untouched, got %s", name, out)
		}
	}
}

// A single-entry catalog has nothing to reorder, but hiding still applies.
func TestSingleEntryCatalogIsUntouchedUnlessHidden(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"only"}]}`)
	if _, changed := curateModelCatalog(body); changed {
		t.Fatal("a single-entry catalog must not be rewritten")
	}

	withConfig(t, settings{Order: orderAscending, Hidden: []string{"only"}})
	out, changed := curateModelCatalog(body)
	if !changed {
		t.Fatal("hiding the only model must still rewrite the catalog")
	}
	if got := sortKeys(t, out, "data"); len(got) != 0 {
		t.Fatalf("catalog = %v, want empty", got)
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

// Hiding must not leak into the request path: a completion for a hidden model
// still has to pass through untouched.
func TestHiddenModelStillCompletes(t *testing.T) {
	withConfig(t, settings{Order: orderAscending, Hidden: []string{"secret"}})
	resp := interceptFor(t, pluginapi.ResponseInterceptRequest{
		Model: "secret",
		Body:  []byte(`{"id":"chatcmpl-1","choices":[{"message":{"content":"ok"}}]}`),
	})
	if len(resp.Body) != 0 {
		t.Fatalf("completion for a hidden model was modified: %s", resp.Body)
	}
}

// Listings arrive with empty Model and RequestedModel, so they get curated.
func TestModelListIsSortedThroughIntercept(t *testing.T) {
	resp := interceptFor(t, pluginapi.ResponseInterceptRequest{
		SourceFormat: "openai",
		Body:         []byte(`{"object":"list","data":[{"id":"zeta"},{"id":"alpha"}]}`),
	})
	if len(resp.Body) == 0 {
		t.Fatal("catalog should have been sorted")
	}
	assertOrder(t, sortKeys(t, resp.Body, "data"), []string{"alpha", "zeta"})
}

func TestApplyConfigParsesYAML(t *testing.T) {
	withConfig(t, settings{})
	request := mustLifecycleRequest(t, "order: desc\npinned:\n  - fast-model\nhidden:\n  - legacy\n")

	if err := applyConfig(request); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if config.Order != orderDescending {
		t.Fatalf("order = %q, want %q", config.Order, orderDescending)
	}
	if len(config.Pinned) != 1 || config.Pinned[0] != "fast-model" {
		t.Fatalf("pinned = %v", config.Pinned)
	}
	if len(config.Hidden) != 1 || config.Hidden[0] != "legacy" {
		t.Fatalf("hidden = %v", config.Hidden)
	}
}

func TestApplyConfigDefaultsWhenBlockIsAbsent(t *testing.T) {
	withConfig(t, settings{Order: orderDescending, Pinned: []string{"stale"}})
	if err := applyConfig(mustLifecycleRequest(t, "")); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	// A removed block must reset to defaults rather than keep the old state.
	if config.Order != orderAscending || config.Pinned != nil || config.Hidden != nil {
		t.Fatalf("config = %+v, want defaults", config)
	}
}

func TestApplyConfigRejectsUnknownOrder(t *testing.T) {
	withConfig(t, settings{})
	if err := applyConfig(mustLifecycleRequest(t, "order: sideways\n")); err == nil {
		t.Fatal("an unknown order value must be rejected")
	}
}

// A rejected configuration must surface as an error envelope rather than a
// crash, so the host logs it instead of fusing the plugin.
func TestRegisterWithInvalidConfigReturnsErrorEnvelope(t *testing.T) {
	withConfig(t, settings{})
	out, err := handleMethod("plugin.register", mustLifecycleRequest(t, "order: sideways\n"))
	if err != nil {
		t.Fatalf("handleMethod must not return a Go error: %v", err)
	}
	var env envelope
	if err = json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "invalid_config" {
		t.Fatalf("expected invalid_config error envelope, got %s", out)
	}
}

func TestApplyConfigTrimsEmptyEntries(t *testing.T) {
	withConfig(t, settings{})
	if err := applyConfig(mustLifecycleRequest(t, "pinned:\n  - \"\"\n  - \"  \"\n")); err != nil {
		t.Fatalf("applyConfig: %v", err)
	}
	if config.Pinned != nil {
		t.Fatalf("pinned = %v, want nil", config.Pinned)
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

// Every advertised field must be one the plugin actually reads, or the panel
// would offer a setting that silently does nothing.
func TestConfigFieldsMatchSettings(t *testing.T) {
	want := map[string]pluginapi.ConfigFieldType{
		"order":  pluginapi.ConfigFieldTypeEnum,
		"pinned": pluginapi.ConfigFieldTypeArray,
		"hidden": pluginapi.ConfigFieldTypeArray,
	}
	fields := pluginRegistration().Metadata.ConfigFields
	if len(fields) != len(want) {
		t.Fatalf("config fields = %d, want %d", len(fields), len(want))
	}
	for _, field := range fields {
		wantType, known := want[field.Name]
		if !known {
			t.Fatalf("unexpected config field %q", field.Name)
		}
		if field.Type != wantType {
			t.Fatalf("field %q type = %q, want %q", field.Name, field.Type, wantType)
		}
		if field.Description == "" {
			t.Fatalf("field %q has no description", field.Name)
		}
	}
	// The enum must offer exactly the values applyConfig accepts.
	for _, field := range fields {
		if field.Name != "order" {
			continue
		}
		if len(field.EnumValues) != 2 ||
			field.EnumValues[0] != orderAscending ||
			field.EnumValues[1] != orderDescending {
			t.Fatalf("order enum = %v", field.EnumValues)
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
