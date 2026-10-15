// Derived from github.com/Wei-Shaw/sub2api, licensed under LGPLv3.
// See LGPL-3.0.txt for the upstream license.
package responsescompat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// EffectiveTools includes declarations introduced by replayed client history.
// The returned JSON retains each declaration's native fields.
func EffectiveTools(tools, input json.RawMessage) (json.RawMessage, error) {
	var declared []any
	if len(tools) > 0 && string(tools) != "null" {
		if err := kitutil.UnmarshalUseNumber(tools, &declared); err != nil {
			return nil, err
		}
	}
	if len(input) == 0 || kitutil.GetJsonType(input) != "array" {
		return kitutil.Marshal(declared)
	}
	var items []any
	if err := kitutil.UnmarshalUseNumber(input, &items); err != nil {
		return nil, fmt.Errorf("parse Responses tools history: %w", err)
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok || item["type"] != "additional_tools" {
			continue
		}
		additional, ok := item["tools"].([]any)
		if !ok {
			return nil, fmt.Errorf("additional_tools must contain a tools array")
		}
		declared = append(declared, additional...)
	}
	promoted, err := promotedResponsesToolSearchDiscoveries(declared, items)
	if err != nil {
		return nil, err
	}
	return kitutil.Marshal(append(declared, promoted...))
}

type ToolMetadata struct {
	Custom     map[string]bool
	Functions  map[string]bool
	Namespaces map[string]ResponsesNamespaceName
	ToolSearch bool
}

type metadataTool struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Tools    []metadataTool `json:"tools"`
	Children []metadataTool `json:"children"`
}

// InspectTools provides the same restoration names as the request encoders.
// Legacy native-dialect aliases remain accepted for replayed provider calls.
func InspectTools(raw json.RawMessage) ToolMetadata {
	var tools []metadataTool
	if kitutil.Unmarshal(raw, &tools) != nil {
		return ToolMetadata{}
	}
	out := ToolMetadata{Custom: map[string]bool{}, Functions: map[string]bool{}, Namespaces: map[string]ResponsesNamespaceName{}}
	var visit func([]metadataTool, string)
	visit = func(tools []metadataTool, namespace string) {
		for _, tool := range tools {
			name := convmeta.NamespacedToolName(namespace, tool.Name)
			switch tool.Type {
			case "custom":
				if tool.Name != "" {
					out.Custom[name] = true
				}
			case "function":
				if tool.Name != "" {
					out.Functions[name] = true
				}
			case "namespace":
				children := tool.Tools
				if len(children) == 0 {
					children = tool.Children
				}
				visit(children, tool.Name)
			case "tool_search":
				out.ToolSearch = true
			}
			if namespace != "" && tool.Name != "" && (tool.Type == "function" || tool.Type == "custom") {
				restore := ResponsesNamespaceName{Namespace: namespace, Name: tool.Name}
				out.Namespaces[name] = restore
				out.Namespaces[flattenNamespaceToolName(namespace, tool.Name)] = restore
			}
		}
	}
	visit(tools, "")
	return out
}

func ExtractReasoningItem(raw json.RawMessage) (id, text string, ok bool) {
	var item map[string]json.RawMessage
	if kitutil.Unmarshal(raw, &item) != nil || metadataRawString(item["type"]) != "reasoning" {
		return "", "", false
	}
	collect := func(raw json.RawMessage) string {
		var text string
		if kitutil.Unmarshal(raw, &text) == nil {
			return text
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if kitutil.Unmarshal(raw, &parts) != nil {
			return ""
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	text = collect(item["summary"])
	if text == "" {
		text = collect(item["content"])
	}
	return metadataRawString(item["id"]), text, true
}

func metadataRawString(raw json.RawMessage) string {
	var text string
	_ = kitutil.Unmarshal(raw, &text)
	return text
}
