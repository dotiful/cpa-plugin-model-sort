// Package main implements a CLIProxyAPI plugin that sorts the model catalog.
//
// CPA builds its catalog by ranging over a Go map, so the order of /v1/models
// is randomized on every process start. Upstream declined to sort it
// (issue #3081 -> discussion #3888, closed), which leaves client model pickers
// reshuffled after each restart.
//
// The plugin sorts the listing in response.intercept_after. Model listings pass
// through the response interceptor chain just like completions do: see
// WriteModelListResponse in sdk/api/handlers/handlers_interceptors.go and the
// coverage in internal/api/server_models_interceptor_test.go.
package main

import (
	"encoding/json"
	"sort"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// pluginID must match the shared library filename, which CPA uses as the
// plugin ID and as the key under plugins.configs.
const pluginID = "model-sort"

// pluginVersion is injected at release build time with
// -ldflags "-X main.pluginVersion=<version>".
var pluginVersion = "0.0.0-dev"

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

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodPluginShutdown:
		return okEnvelope(map[string]any{})
	case pluginabi.MethodResponseInterceptAfter:
		return interceptResponse(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
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
		},
		Capabilities: registrationCapability{ResponseInterceptor: true},
	}
}

// interceptResponse sorts model listings and leaves every other response
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
	sorted, changed := sortModelCatalog(req.Body)
	if !changed {
		return okEnvelope(pluginapi.ResponseInterceptResponse{})
	}
	return okEnvelope(pluginapi.ResponseInterceptResponse{Body: sorted})
}

// sortModelCatalog sorts the model array inside a catalog body. It handles the
// shapes CPA emits:
//
//	OpenAI and Claude: {"object":"list","data":[{"id":...}]}
//	Gemini:            {"models":[{"name":"models/..."}]}
//
// It reports whether the body changed. Any unexpected shape is left alone.
func sortModelCatalog(body []byte) ([]byte, bool) {
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
	if len(items) < 2 {
		return nil, false
	}

	// Every entry must carry a sort key. Otherwise sorting would reorder the
	// catalog on an empty string, so leave the body untouched instead.
	for _, item := range items {
		if modelSortKey(item) == "" {
			return nil, false
		}
	}

	if sort.SliceIsSorted(items, func(i, j int) bool {
		return modelSortKey(items[i]) < modelSortKey(items[j])
	}) {
		return nil, false
	}
	sort.SliceStable(items, func(i, j int) bool {
		return modelSortKey(items[i]) < modelSortKey(items[j])
	})

	encoded, errMarshal := json.Marshal(items)
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

// modelSortKey returns the identifier the catalog is ordered by. The OpenAI and
// Claude formats key on "id" while the Gemini format keys on "name", so an
// id-only comparator would silently leave the Gemini listing unsorted.
func modelSortKey(model map[string]any) string {
	if model == nil {
		return ""
	}
	if id, ok := model["id"].(string); ok && id != "" {
		return id
	}
	if name, ok := model["name"].(string); ok {
		return name
	}
	return ""
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
