// Portions derived from github.com/Wei-Shaw/sub2api, licensed under LGPLv3.
// See LGPL-3.0.txt in this package for the upstream license.
package responsescompat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ResponsesClientToolMapping records the reversible lowering applied before a
// native Responses request is sent to an upstream that only understands
// function tools.
type ResponsesClientToolMapping struct {
	CustomTools    map[string]bool
	ToolSearch     bool
	NamespaceTools map[string]ResponsesNamespaceName
}

// AdaptResponsesClientTools lowers Codex client-only tools in req to
// ordinary function tools. It mutates req and returns the mapping required to
// restore the upstream response.
func AdaptResponsesClientTools(req map[string]any) (ResponsesClientToolMapping, bool, error) {
	if req == nil {
		return ResponsesClientToolMapping{}, false, nil
	}
	tools, ok := req["tools"].([]any)
	if !ok || len(tools) == 0 {
		return ResponsesClientToolMapping{}, false, nil
	}
	discovered, err := promoteResponsesToolSearchDiscoveries(req)
	if err != nil {
		return ResponsesClientToolMapping{}, false, err
	}
	if discovered {
		tools, _ = req["tools"].([]any)
	}

	adapter := ResponsesClientToolMapping{CustomTools: make(map[string]bool)}
	functionNames := make(map[string]bool)
	customNames := make(map[string]bool)
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringValue(tool["name"]))
		switch strings.TrimSpace(stringValue(tool["type"])) {
		case "function":
			if name != "" {
				functionNames[name] = true
			}
		case "custom":
			if name != "" {
				customNames[name] = true
			}
		case "tool_search":
			adapter.ToolSearch = true
		}
	}
	for name := range customNames {
		if functionNames[name] {
			return ResponsesClientToolMapping{}, false, fmt.Errorf("custom tool %q conflicts with a function tool of the same name; this upstream cannot disambiguate them, rename one of the tools", name)
		}
	}
	if adapter.ToolSearch && (functionNames[toolSearchProxyName] || customNames[toolSearchProxyName]) {
		return ResponsesClientToolMapping{}, false, fmt.Errorf("built-in tool_search conflicts with a declared tool named %q; this upstream cannot disambiguate them, rename the tool", toolSearchProxyName)
	}

	// Namespace flattening also rewrites namespace-qualified history and choice.
	names, flattened, err := FlattenResponsesNamespaces(req)
	if err != nil {
		return ResponsesClientToolMapping{}, false, err
	}
	adapter.NamespaceTools = names
	if adapter.ToolSearch {
		if _, exists := names[toolSearchProxyName]; exists {
			return ResponsesClientToolMapping{}, false, fmt.Errorf("built-in tool_search conflicts with namespace tool flattened as %q; this upstream cannot disambiguate them, rename the tool", toolSearchProxyName)
		}
	}

	tools, _ = req["tools"].([]any)
	lowered := make([]any, 0, len(tools))
	changed := discovered || flattened
	seenSearch := false
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			lowered = append(lowered, raw)
			continue
		}
		typ := strings.TrimSpace(stringValue(tool["type"]))
		name := strings.TrimSpace(stringValue(tool["name"]))
		switch typ {
		case "custom":
			if name == "" {
				lowered = append(lowered, raw)
				continue
			}
			copy := copyClientTool(tool)
			copy["type"] = "function"
			copy["parameters"] = json.RawMessage(customToolInputSchema)
			delete(copy, "format")
			adapter.CustomTools[name] = true
			lowered = append(lowered, copy)
			changed = true
		case "tool_search":
			if seenSearch {
				changed = true
				continue
			}
			seenSearch = true
			lowered = append(lowered, map[string]any{
				"type": "function", "name": toolSearchProxyName,
				"description": "Search and load Codex tools, plugins, connectors, and MCP namespaces for the current task.",
				"parameters":  json.RawMessage(toolSearchProxySchema),
			})
			changed = true
		default:
			lowered = append(lowered, raw)
		}
	}
	if stripResponsesDeferredToolFlags(lowered) {
		changed = true
	}
	if changed {
		req["tools"] = lowered
	}
	historyChanged, err := rewriteClientToolHistory(req["input"], &adapter)
	if err != nil {
		return ResponsesClientToolMapping{}, false, err
	}
	if historyChanged {
		changed = true
	}
	if rewriteClientToolChoice(req, &adapter) {
		changed = true
	}
	if len(adapter.CustomTools) == 0 {
		adapter.CustomTools = nil
	}
	if len(adapter.NamespaceTools) == 0 {
		adapter.NamespaceTools = nil
	}
	return adapter, changed, nil
}

// stripResponsesDeferredToolFlags removes defer_loading only when the final
// declaration list no longer contains the built-in tool_search it requires.
func stripResponsesDeferredToolFlags(tools []any) bool {
	if hasResponsesToolSearchDeclaration(tools) {
		return false
	}
	changed := false
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := tool["defer_loading"]; exists {
			delete(tool, "defer_loading")
			changed = true
		}
	}
	return changed
}

// AdaptResponsesClientToolsWithInheritedMapping lowers client-tool history on
// a follow-up request that omits the session-level tools declaration. An
// explicitly present tools field, including an empty or malformed value,
// always replaces the inherited mapping and is handled by the ordinary
// declaration-driven adapter.
func AdaptResponsesClientToolsWithInheritedMapping(
	req map[string]any,
	inherited ResponsesClientToolMapping,
	inheritedLoweredTools ...[]any,
) (ResponsesClientToolMapping, bool, error) {
	if req == nil {
		return ResponsesClientToolMapping{}, false, nil
	}
	if _, toolsPresent := req["tools"]; toolsPresent {
		return AdaptResponsesClientTools(req)
	}
	if len(inherited.CustomTools) == 0 && !inherited.ToolSearch && len(inherited.NamespaceTools) == 0 {
		return ResponsesClientToolMapping{}, false, nil
	}
	if len(inheritedLoweredTools) > 0 && len(inheritedLoweredTools[0]) > 0 {
		req["tools"] = restoreInheritedResponsesClientToolDeclarations(inheritedLoweredTools[0], inherited)
		return AdaptResponsesClientTools(req)
	}

	changed, err := rewriteClientToolHistory(req["input"], &inherited)
	if err != nil {
		return ResponsesClientToolMapping{}, false, err
	}
	if len(inherited.NamespaceTools) > 0 {
		before := changed
		rewriteNamespaceQualifiedCalls(req["input"], inherited.NamespaceTools)
		// Namespace rewriting does not currently report whether it changed a
		// value. A retained namespace mapping is only used for follow-up
		// history, so conservatively rebuild the request when input exists.
		if _, inputPresent := req["input"]; inputPresent && !before {
			changed = true
		}
	}
	if rewriteClientToolChoice(req, &inherited) {
		changed = true
	}
	return inherited, changed, nil
}

func copyClientTool(tool map[string]any) map[string]any {
	copy := make(map[string]any, len(tool))
	for key, value := range tool {
		copy[key] = value
	}
	return copy
}

func rewriteClientToolHistory(value any, adapter *ResponsesClientToolMapping) (bool, error) {
	changed := false
	var visit func(any) error
	visit = func(value any) error {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				if err := visit(item); err != nil {
					return err
				}
			}
		case map[string]any:
			typ := strings.TrimSpace(stringValue(typed["type"]))
			switch typ {
			case "custom_tool_call":
				if adapter.CustomTools[strings.TrimSpace(stringValue(typed["name"]))] {
					typed["type"] = "function_call"
					typed["arguments"] = customToolCallArguments(stringValue(typed["input"]))
					delete(typed, "input")
					normalizeLoweredFunctionItemID(typed)
					changed = true
				}
			case "custom_tool_call_output":
				typed["type"] = "function_call_output"
				normalizeLoweredFunctionItemID(typed)
				normalizeClientToolOutput(typed)
				changed = true
			case "tool_search_call":
				if adapter.ToolSearch {
					typed["type"] = "function_call"
					typed["name"] = toolSearchProxyName
					typed["arguments"] = rawObjectString(typed["arguments"])
					delete(typed, "execution")
					normalizeLoweredFunctionItemID(typed)
					changed = true
				}
			case "tool_search_output":
				if adapter.ToolSearch {
					callID := strings.TrimSpace(stringValue(typed["call_id"]))
					if callID == "" {
						return fmt.Errorf("tool_search_output requires a non-empty string call_id before it can be lowered to function_call_output")
					}
					typed["type"] = "function_call_output"
					normalizeLoweredFunctionItemID(typed)
					if err := normalizeToolSearchOutput(typed); err != nil {
						return err
					}
					changed = true
				}
			}
			for _, child := range typed {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(value); err != nil {
		return false, err
	}
	return changed, nil
}

// normalizeLoweredFunctionItemID reconciles Codex client-only item IDs such as
// ctc_*, ctco_*, tsc_*, and tso_* after their item type is lowered to the
// function protocol, which validates these IDs with the fc prefix. A ctc_/tsc_
// call ID maps back to the fc_ ID it was raised from; the remaining IDs have no
// function-protocol counterpart and are dropped. call_id, which is preserved
// separately, stays the tool call/output pairing key either way.
func normalizeLoweredFunctionItemID(item map[string]any) {
	id := strings.TrimSpace(stringValue(item["id"]))
	if id == "" || strings.HasPrefix(id, "fc") {
		return
	}
	// A ctc_/tsc_ call ID on a lowered call item is the one we minted from the
	// upstream's own fc_ ID when the item was raised, so map it back instead of
	// dropping it. Anything else has no known function-protocol counterpart.
	if recovered := retypedResponsesToolCallItemID(id, "function_call"); recovered != id {
		item["id"] = recovered
		return
	}
	delete(item, "id")
}

// responsesToolCallItemIDPrefixes lists the Responses item ID prefixes that are
// tied to a specific tool-call item type.
var responsesToolCallItemIDPrefixes = []string{"fc_", "ctc_", "tsc_"}

// responsesToolCallItemIDPrefix reports the ID prefix the Responses API
// validates for itemType, or "" when the type constrains no prefix.
func responsesToolCallItemIDPrefix(itemType string) string {
	switch itemType {
	case "custom_tool_call":
		return "ctc_"
	case "tool_search_call":
		return "tsc_"
	case "function_call":
		return "fc_"
	default:
		return ""
	}
}

// retypedResponsesToolCallItemID re-prefixes an upstream item ID so it agrees
// with the item type we raise it back to. A function-only upstream answers a
// lowered custom tool with an fc_ item ID; emitting that ID on the restored
// custom_tool_call poisons the client's history, because a later replay of the
// same item to an upstream that validates IDs fails with
// "Invalid 'input[N].id' ... Expected an ID that begins with 'ctc'".
// The suffix is preserved so the ID stays stable and unique per upstream item.
// IDs that carry no known tool-call prefix are left alone rather than guessed at.
func retypedResponsesToolCallItemID(id, itemType string) string {
	want := responsesToolCallItemIDPrefix(itemType)
	if want == "" || id == "" || strings.HasPrefix(id, want) {
		return id
	}
	for _, known := range responsesToolCallItemIDPrefixes {
		if known != want && strings.HasPrefix(id, known) {
			return want + strings.TrimPrefix(id, known)
		}
	}
	return id
}

// retypeResponsesToolCallItemID applies retypedResponsesToolCallItemID to a
// decoded item map.
func retypeResponsesToolCallItemID(item map[string]any, itemType string) {
	id := strings.TrimSpace(stringValue(item["id"]))
	if retyped := retypedResponsesToolCallItemID(id, itemType); retyped != id {
		item["id"] = retyped
	}
}

func normalizeClientToolOutput(item map[string]any) {
	output, exists := item["output"]
	if !exists {
		return
	}
	if _, ok := output.(string); ok {
		return
	}
	if isResponsesToolOutputContent(output) {
		return
	}
	if output == nil {
		item["output"] = ""
		return
	}
	encoded, err := kitutil.Marshal(output)
	if err != nil {
		item["output"] = ""
		return
	}
	item["output"] = string(encoded)
}

func isResponsesToolOutputContent(output any) bool {
	parts, ok := output.([]any)
	if !ok || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		typed, ok := part.(map[string]any)
		if !ok {
			return false
		}
		switch stringValue(typed["type"]) {
		case "input_text", "input_image", "input_file":
		default:
			return false
		}
	}
	return true
}

// normalizeToolSearchOutput converts both tool_search output wire shapes into
// the string output required by function_call_output. Older clients send an
// output field directly; newer Codex clients return discovered definitions in
// a top-level tools field. Codex treats that field's value as the tool output,
// so serialize the value directly rather than wrapping it in another object.
func normalizeToolSearchOutput(item map[string]any) error {
	if output, hasOutput := item["output"]; hasOutput {
		switch typed := output.(type) {
		case string:
			item["output"] = typed
		case nil:
			item["output"] = ""
		default:
			encoded, err := kitutil.Marshal(typed)
			if err != nil {
				return fmt.Errorf("tool_search_output output cannot be encoded as function_call_output output: %w", err)
			}
			item["output"] = string(encoded)
		}
		dropToolSearchOutputPrivateFields(item)
		return nil
	}
	tools, hasTools := item["tools"]
	if !hasTools {
		return fmt.Errorf("tool_search_output requires output or tools before it can be lowered to function_call_output")
	}
	encoded, err := kitutil.Marshal(tools)
	if err != nil {
		return fmt.Errorf("tool_search_output tools cannot be encoded as function_call_output output: %w", err)
	}
	item["output"] = string(encoded)
	dropToolSearchOutputPrivateFields(item)
	return nil
}

func dropToolSearchOutputPrivateFields(item map[string]any) {
	delete(item, "tools")
	delete(item, "status")
	delete(item, "execution")
}

func rewriteClientToolChoice(req map[string]any, adapter *ResponsesClientToolMapping) bool {
	choice, ok := req["tool_choice"].(map[string]any)
	if !ok {
		return false
	}
	typ := strings.TrimSpace(stringValue(choice["type"]))
	name := strings.TrimSpace(stringValue(choice["name"]))
	if typ == "custom" && adapter.CustomTools[name] {
		choice["type"] = "function"
		return true
	}
	if typ == "tool_search" && adapter.ToolSearch {
		req["tool_choice"] = map[string]any{"type": "function", "name": toolSearchProxyName}
		return true
	}
	return false
}

func customToolCallArguments(input string) string {
	encoded, _ := kitutil.Marshal(map[string]string{"input": input})
	return string(encoded)
}

func rawObjectString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := kitutil.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// RestoreResponsesClientToolPayload restores client tool calls in a non-stream
// native Responses JSON payload.
func RestoreResponsesClientToolPayload(payload []byte, mapping ResponsesClientToolMapping) ([]byte, bool, error) {
	if len(payload) == 0 {
		return payload, false, nil
	}
	var value any
	if err := kitutil.UnmarshalUseNumber(payload, &value); err != nil {
		return payload, false, err
	}
	changed := restoreClientToolValue(value, &mapping)
	if !changed {
		if len(mapping.NamespaceTools) == 0 {
			return payload, false, nil
		}
		return RestoreResponsesNamespaceCalls(payload, mapping.NamespaceTools)
	}
	rebuiltPayload, err := kitutil.MarshalWithoutHTMLEscape(value)
	if err != nil {
		return payload, false, err
	}

	if len(mapping.NamespaceTools) == 0 {
		return rebuiltPayload, true, nil
	}
	restored, _, err := RestoreResponsesNamespaceCalls(rebuiltPayload, mapping.NamespaceTools)
	if err != nil {
		return payload, false, err
	}
	return restored, true, nil
}

func restoreClientToolValue(value any, adapter *ResponsesClientToolMapping) bool {
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = restoreClientToolValue(item, adapter) || changed
		}
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) == "function_call" {
			name := strings.TrimSpace(stringValue(typed["name"]))
			if adapter.CustomTools[name] {
				typed["type"] = "custom_tool_call"
				retypeResponsesToolCallItemID(typed, "custom_tool_call")
				typed["input"] = extractCustomToolCallInput(rawObjectString(typed["arguments"]))
				delete(typed, "arguments")
				delete(typed, "namespace")
				changed = true
			} else if adapter.ToolSearch && name == toolSearchProxyName {
				typed["type"] = "tool_search_call"
				retypeResponsesToolCallItemID(typed, "tool_search_call")
				typed["execution"] = "client"
				typed["arguments"] = json.RawMessage(toolSearchCallArgumentsJSON(rawObjectString(typed["arguments"])))
				delete(typed, "name")
				delete(typed, "namespace")
				changed = true
			}
		}
		for _, child := range typed {
			changed = restoreClientToolValue(child, adapter) || changed
		}
	}
	return changed
}
