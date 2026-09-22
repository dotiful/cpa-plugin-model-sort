// Package main implements a CLIProxyAPI plugin that curates the model catalog.
//
// CPA builds its catalog by ranging over a Go map, so the order of /v1/models
// is randomized on every process start. Upstream declined to sort it
// (issue #3081 -> discussion #3888, closed), which leaves client model pickers
// reshuffled after each restart.
//
// The plugin sorts the listing in response.intercept_after, and can optionally
// pin entries to the top or hide them from the catalog. Model listings pass
// through the response interceptor chain just like completions do: see
// WriteModelListResponse in sdk/api/handlers/handlers_interceptors.go and the
// coverage in internal/api/server_models_interceptor_test.go.
package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

// pluginID must match the shared library filename, which CPA uses as the
// plugin ID and as the key under plugins.configs.
const pluginID = "model-sort"

// pluginVersion is injected at release build time with
// -ldflags "-X main.pluginVersion=<version>".
var pluginVersion = "0.0.0-dev"

const (
	orderAscending  = "asc"
	orderDescending = "desc"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

// registrationCapability declares exactly one capability. Declaring a
// capability the plugin does not implement makes the host call a method that
// does not exist.
type registrationCapability struct {
	ResponseInterceptor bool `json:"response_interceptor"`
}

// lifecycleRequest is what the host sends with register and reconfigure.
// ConfigYAML carries the plugin's own block under plugins.configs.<id>; it is
// a []byte on the host side, so JSON transports it base64-encoded.
type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

// settings mirrors the plugin's YAML block. Every field is optional, and the
// zero value is the documented default: ascending order, nothing pinned,
// nothing hidden.
type settings struct {
	Order  string   `yaml:"order"`
	Pinned []string `yaml:"pinned"`
	Hidden []string `yaml:"hidden"`
}

// config is the active configuration. The host calls reconfigure on config
// changes, and plugin calls are serialized by the host, so a plain variable is
// sufficient here.
var config = settings{Order: orderAscending}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errApply := applyConfig(request); errApply != nil {
			return errorEnvelope("invalid_config", errApply.Error()), nil
		}
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodPluginShutdown:
		return okEnvelope(map[string]any{})
	case pluginabi.MethodResponseInterceptAfter:
		return interceptResponse(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

// applyConfig parses the plugin's YAML block. An empty or absent block resets
// the configuration to its defaults, so removing a key in config.yaml takes
// effect on reconfigure rather than lingering from the previous state.
func applyConfig(request []byte) error {
	parsed := settings{}
	if len(request) > 0 {
		var lifecycle lifecycleRequest
		if errUnmarshal := json.Unmarshal(request, &lifecycle); errUnmarshal != nil {
			return errUnmarshal
		}
		if len(lifecycle.ConfigYAML) > 0 {
			if errYAML := yaml.Unmarshal(lifecycle.ConfigYAML, &parsed); errYAML != nil {
				return errYAML
			}
		}
	}

	switch strings.ToLower(strings.TrimSpace(parsed.Order)) {
	case "", orderAscending:
		parsed.Order = orderAscending
	case orderDescending:
		parsed.Order = orderDescending
	default:
		return fmt.Errorf("order must be %q or %q, got %q", orderAscending, orderDescending, parsed.Order)
	}
	parsed.Pinned = cleanList(parsed.Pinned)
	parsed.Hidden = cleanList(parsed.Hidden)

	config = parsed
	return nil
}

// cleanList trims entries and drops empties, so a stray "- " in YAML cannot
// pin or hide the empty model ID.
func cleanList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			// validPlugin in internal/pluginhost/host.go requires all four
			// fields to be non-empty, GitHubRepository included. Leaving any
			// of them empty yields a plugin that logs "plugin loaded" but
			// never "plugin registered".
			Name:             pluginID,
			Version:          pluginVersion,
			Author:           "dotiful",
			GitHubRepository: "https://github.com/dotiful/cpa-plugin-model-sort",
			Logo:             "https://raw.githubusercontent.com/dotiful/cpa-plugin-model-sort/main/logo.png",
			ConfigFields:     configFields(),
		},
		Capabilities: registrationCapability{ResponseInterceptor: true},
	}
}

// configFields tells management clients which settings this plugin owns so the
// panel can render a form for them.
func configFields() []pluginapi.ConfigField {
	return []pluginapi.ConfigField{
		{
			Name:        "order",
			Type:        pluginapi.ConfigFieldTypeEnum,
			EnumValues:  []string{orderAscending, orderDescending},
			Description: "Catalog sort direction. Defaults to asc.",
		},
		{
			Name:        "pinned",
			Type:        pluginapi.ConfigFieldTypeArray,
			Description: "Model IDs kept at the top of the catalog, in the order listed here. Supports * wildcards.",
		},
		{
			Name:        "hidden",
			Type:        pluginapi.ConfigFieldTypeArray,
			Description: "Model IDs removed from catalog responses. Supports * wildcards. Hidden models stay requestable.",
		},
	}
}

// interceptResponse curates model listings and leaves every other response
// untouched. An empty Body means "unchanged", so anything uncertain returns an
// empty response.
func interceptResponse(raw []byte) ([]byte, error) {
	var req pluginapi.ResponseInterceptRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}

	// A model listing arrives without a model: WriteModelListResponse passes
	// empty strings for both fields, while any real completion fills Model.
	// This pair of checks keeps all chat traffic out of the sorting path.
	if req.Model != "" || req.RequestedModel != "" || req.Stream {
		return okEnvelope(pluginapi.ResponseInterceptResponse{})
	}
	curated, changed := curateModelCatalog(req.Body)
	if !changed {
		return okEnvelope(pluginapi.ResponseInterceptResponse{})
	}
	return okEnvelope(pluginapi.ResponseInterceptResponse{Body: curated})
}

// curateModelCatalog hides, orders and pins the model array inside a catalog
// body. It handles the shapes CPA emits:
//
//	OpenAI, Claude, Grok: {"object":"list","data":[{"id":...}]}
//	Gemini:               {"models":[{"name":"models/..."}]}
//	Codex client:         {"models":[{"slug":...}]}
//
// It reports whether the body changed. Any unexpected shape is left alone.
func curateModelCatalog(body []byte) ([]byte, bool) {
	if len(body) == 0 {
		return nil, false
	}
	var root map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(body, &root); errUnmarshal != nil {
		return nil, false
	}

	key := ""
	for _, candidate := range []string{"data", "models"} {
		if _, ok := root[candidate]; ok {
			key = candidate
			break
		}
	}
	if key == "" {
		return nil, false
	}

	var items []map[string]any
	if errUnmarshal := json.Unmarshal(root[key], &items); errUnmarshal != nil {
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}

	// Every entry must carry a sort key. Otherwise ordering would compare on an
	// empty string, so leave the body untouched instead.
	for _, item := range items {
		if modelSortKey(item) == "" {
			return nil, false
		}
	}

	curated := applyHidden(items)
	curated = applyOrder(curated)
	curated = applyPinned(curated)

	if sameOrder(items, curated) {
		return nil, false
	}

	encoded, errMarshal := json.Marshal(curated)
	if errMarshal != nil {
		return nil, false
	}
	root[key] = encoded
	out, errMarshal := json.Marshal(root)
	if errMarshal != nil {
		return nil, false
	}
	return out, true
}

// applyHidden drops configured models from the catalog. Hidden models remain
// routable: this only edits the listing, never the request path.
func applyHidden(items []map[string]any) []map[string]any {
	if len(config.Hidden) == 0 {
		return items
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if matchesAny(config.Hidden, modelIdentity(item)) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// matchesAny reports whether a model ID matches any of the configured patterns.
func matchesAny(patterns []string, id string) bool {
	for _, pattern := range patterns {
		if matchWildcard(pattern, id) {
			return true
		}
	}
	return false
}

// matchWildcard matches a model ID against a pattern where "*" stands for any
// run of characters. The semantics deliberately mirror CPA's own matcher in
// sdk/cliproxy/service_models.go, which backs oauth-excluded-models, so a
// pattern behaves the same whether it is written for the host or for this
// plugin. A pattern without "*" is an exact comparison.
func matchWildcard(pattern, value string) bool {
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}

	parts := strings.Split(pattern, "*")
	if prefix := parts[0]; prefix != "" {
		if !strings.HasPrefix(value, prefix) {
			return false
		}
		value = value[len(prefix):]
	}
	if suffix := parts[len(parts)-1]; suffix != "" {
		if !strings.HasSuffix(value, suffix) {
			return false
		}
		value = value[:len(value)-len(suffix)]
	}
	// Middle segments must appear in order, each after the previous one.
	for index := 1; index < len(parts)-1; index++ {
		segment := parts[index]
		if segment == "" {
			continue
		}
		at := strings.Index(value, segment)
		if at < 0 {
			return false
		}
		value = value[at+len(segment):]
	}
	return true
}

func applyOrder(items []map[string]any) []map[string]any {
	out := append([]map[string]any(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		if config.Order == orderDescending {
			return modelSortKey(out[i]) > modelSortKey(out[j])
		}
		return modelSortKey(out[i]) < modelSortKey(out[j])
	})
	return out
}

// applyPinned moves configured models to the front, in the order they are
// listed in the configuration. A pattern pins every model it matches, keeping
// their relative catalog order. Patterns that match nothing, and IDs that are
// absent or hidden, are ignored rather than treated as an error.
func applyPinned(items []map[string]any) []map[string]any {
	if len(config.Pinned) == 0 {
		return items
	}
	remaining := append([]map[string]any(nil), items...)
	out := make([]map[string]any, 0, len(items))
	for _, pattern := range config.Pinned {
		for index, item := range remaining {
			if item == nil || !matchWildcard(pattern, modelIdentity(item)) {
				continue
			}
			out = append(out, item)
			remaining[index] = nil
		}
	}
	for _, item := range remaining {
		if item != nil {
			out = append(out, item)
		}
	}
	return out
}

// sameOrder reports whether two catalogs hold the same entries in the same
// order, so an already-curated body is not rewritten.
func sameOrder(before, after []map[string]any) bool {
	if len(before) != len(after) {
		return false
	}
	for index := range before {
		if modelSortKey(before[index]) != modelSortKey(after[index]) {
			return false
		}
	}
	return true
}

// modelSortKey returns the identifier the catalog is ordered by. Each listing
// format names that field differently, so an id-only comparator would silently
// leave the Gemini and Codex-client listings unsorted:
//
//	OpenAI, Claude, Grok: "id"
//	Codex client:         "slug"
//	Gemini:               "name"
func modelSortKey(model map[string]any) string {
	if model == nil {
		return ""
	}
	for _, field := range []string{"id", "slug", "name"} {
		if value, ok := model[field].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// modelIdentity returns the ID a user would write in the configuration. The
// Gemini catalog reports "models/gemini-3-pro", so the bare ID is matched too
// and users do not have to know which format a listing uses. The prefix is only
// stripped for the Gemini shape, where the sort key comes from "name".
func modelIdentity(model map[string]any) string {
	key := modelSortKey(model)
	if _, keyed := model["id"]; !keyed {
		if _, slugged := model["slug"]; !slugged {
			if trimmed := strings.TrimPrefix(key, "models/"); trimmed != "" {
				return trimmed
			}
		}
	}
	return key
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}
