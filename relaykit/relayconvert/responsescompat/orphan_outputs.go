// Portions derived from github.com/Wei-Shaw/sub2api, licensed under LGPLv3.
// See LGPL-3.0.txt in this package for the upstream license.
package responsescompat

import "strings"

// DropOrphanToolOutputs removes outputs with no call or item reference in input.
// Callers must only apply it when the current input is the complete conversation
// context; a previous_response_id may reference calls outside this input.
// Named function outputs without a call ID are standalone external inputs.
// Retained items, unknown fields, and input order are preserved without mutation.
func DropOrphanToolOutputs(input []any) ([]any, bool) {
	if len(input) == 0 {
		return input, false
	}
	contextIDs := make(map[string]struct{}, len(input))
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.TrimSpace(stringValue(item["type"]))
		id := ""
		if kind == "item_reference" {
			id = strings.TrimSpace(stringValue(item["id"]))
		} else if orphanToolCallContextType(kind) {
			id = strings.TrimSpace(stringValue(item["call_id"]))
			if id == "" {
				id = strings.TrimSpace(stringValue(item["id"]))
			}
		}
		if id != "" {
			contextIDs[id] = struct{}{}
		}
	}
	out := make([]any, 0, len(input))
	changed := false
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		kind := strings.TrimSpace(stringValue(item["type"]))
		if !orphanToolCallOutputType(kind) {
			out = append(out, raw)
			continue
		}
		callID := strings.TrimSpace(stringValue(item["call_id"]))
		standalone := callID == "" && kind == "function_call_output" && strings.TrimSpace(stringValue(item["name"])) != ""
		_, paired := contextIDs[callID]
		if standalone || (callID != "" && paired) {
			out = append(out, raw)
			continue
		}
		changed = true
	}
	if !changed {
		return input, false
	}
	return out, true
}

func orphanToolCallContextType(kind string) bool {
	switch kind {
	case "tool_call", "function_call", "local_shell_call", "tool_search_call", "custom_tool_call", "mcp_tool_call":
		return true
	default:
		return false
	}
}

func orphanToolCallOutputType(kind string) bool {
	switch kind {
	case "function_call_output", "tool_search_output", "custom_tool_call_output", "mcp_tool_call_output":
		return true
	default:
		return false
	}
}
