package toolconv

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func ResponsesHasToolSearch(set Set) bool {
	for _, definition := range set.Definitions {
		if definition.NativeType == "tool_search" {
			return true
		}
	}
	return false
}

// Tool search is executed by the Responses client. Other protocols represent
// it with a callable function; unlike hosted search, it must not disappear.
func lowerResponsesToolSearch(set Set, target types.RelayFormat) Set {
	if set.Source != types.RelayFormatOpenAIResponses || target == types.RelayFormatOpenAIResponses {
		return set
	}
	set.Definitions = append([]Definition(nil), set.Definitions...)
	for i, definition := range set.Definitions {
		if definition.NativeType != "tool_search" {
			continue
		}
		set.Definitions[i] = Definition{Kind: KindFunction, Execution: ExecutionClient, Name: convmeta.ToolSearchFunctionName, NativeType: "tool_search", Function: &Function{Name: convmeta.ToolSearchFunctionName, Description: "Search and load tools, connectors, and namespaces for the current task.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}, "required": []string{"query"}}}}
	}
	if set.Choice != nil && set.Choice.NativeType == "tool_search" && ResponsesHasToolSearch(set) {
		set.Choice = &Choice{Mode: ChoiceNamed, Kind: KindFunction, Name: convmeta.ToolSearchFunctionName}
	}
	return set
}

// A completed tool search can make new client tools available for subsequent
// calls. Promote those declarations before selecting the outgoing protocol,
// and reject incompatible definitions rather than bind calls ambiguously.
func normalizeResponsesToolSearch(input json.RawMessage, set *Set) (json.RawMessage, error) {
	if len(input) == 0 || kitutil.GetJsonType(input) != "array" {
		return input, nil
	}
	var items []map[string]any
	if err := kitutil.UnmarshalUseNumber(input, &items); err != nil {
		return nil, err
	}
	hasSearch := ResponsesHasToolSearch(*set)
	for _, item := range items {
		typ := kitutil.Interface2String(item["type"])
		if typ == "tool_search_output" && hasSearch {
			status, exists := item["status"]
			if !exists || status == "completed" {
				if discoveries, ok := item["tools"].([]any); ok {
					for _, discovery := range discoveries {
						tool, ok := discovery.(map[string]any)
						if !ok {
							continue
						}
						typ := kitutil.Interface2String(tool["type"])
						if typ != "function" && typ != "custom" && typ != "namespace" {
							continue
						}
						raw, err := kitutil.Marshal(tool)
						if err != nil {
							return nil, err
						}
						definitions, err := decodeOpenAIResponsesDefinitions(raw)
						if err != nil {
							return nil, err
						}
						for _, definition := range definitions {
							name := definition.Name
							if name == "" && definition.Function != nil {
								name = definition.Function.Name
							}
							if name == "" {
								var value map[string]any
								_ = kitutil.Unmarshal(definition.Raw, &value)
								name = kitutil.Interface2String(value["name"])
							}
							if strings.TrimSpace(name) == "" {
								continue
							}
							duplicate := false
							for _, existing := range set.Definitions {
								existingName := existing.Name
								if existingName == "" && existing.Function != nil {
									existingName = existing.Function.Name
								}
								if existingName == "" {
									var value map[string]any
									_ = kitutil.Unmarshal(existing.Raw, &value)
									existingName = kitutil.Interface2String(value["name"])
								}
								if existingName != name {
									continue
								}
								if !sameDiscoveredDefinition(existing, definition) {
									return nil, fmt.Errorf("discovered tool %q conflicts with an existing declaration", name)
								}
								duplicate = true
								break
							}
							if !duplicate {
								set.Definitions = append(set.Definitions, definition)
							}
						}
					}
				}
			}
		}
		switch typ {
		case "tool_search_call":
			item["type"] = "function_call"
			item["name"] = convmeta.ToolSearchFunctionName
			arguments := item["arguments"]
			if arguments == nil {
				item["arguments"] = "{}"
			} else if _, ok := arguments.(string); !ok {
				raw, err := kitutil.Marshal(arguments)
				if err != nil {
					return nil, err
				}
				item["arguments"] = string(raw)
			}
		case "tool_search_output":
			item["type"] = "function_call_output"
			if item["output"] == nil {
				item["output"] = item["tools"]
			}
		}
	}
	return kitutil.Marshal(items)
}

func sameDiscoveredDefinition(a, b Definition) bool {
	if a.Kind != b.Kind || a.Namespace != b.Namespace || a.NativeType != b.NativeType {
		return false
	}
	if a.Function != nil || b.Function != nil {
		return reflect.DeepEqual(a.Function, b.Function)
	}
	var av, bv map[string]any
	if kitutil.Unmarshal(a.Raw, &av) != nil || kitutil.Unmarshal(b.Raw, &bv) != nil {
		return false
	}
	delete(av, "defer_loading")
	delete(bv, "defer_loading")
	return reflect.DeepEqual(av, bv)
}
