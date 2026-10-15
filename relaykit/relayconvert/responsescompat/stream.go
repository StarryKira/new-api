package responsescompat

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ResponsesClientToolStreamRestorer restores only the client-tool dialect of
// native Responses SSE. Raw maps retain provider fields unknown to RelayKit.
type ResponsesClientToolStreamRestorer struct {
	adapter  ResponsesClientToolMapping
	nextSeq  int
	seenSeq  bool
	calls    map[string]*streamCall
	byOutput map[int]*streamCall
}
type streamCall struct {
	kind, name, callID, itemID, clientItemID string
	outputIndex                              int
	arguments                                strings.Builder
}

func NewResponsesClientToolStreamRestorer(mapping ResponsesClientToolMapping) *ResponsesClientToolStreamRestorer {
	return &ResponsesClientToolStreamRestorer{adapter: mapping, calls: map[string]*streamCall{}, byOutput: map[int]*streamCall{}}
}

// RestoreEvent accepts a single SSE data payload. A custom arguments.done
// event expands into input.delta and input.done; argument fragments remain
// buffered until a complete freeform input can be decoded.
func (r *ResponsesClientToolStreamRestorer) RestoreEvent(payload []byte) ([][]byte, bool, error) {
	if len(payload) == 0 {
		return nil, false, nil
	}
	if r == nil {
		return [][]byte{payload}, false, nil
	}
	var event map[string]any
	if err := kitutil.Unmarshal(payload, &event); err != nil {
		return nil, false, err
	}
	sequence := eventIndex(event, "sequence_number")
	if !r.seenSeq {
		r.nextSeq = sequence
		r.seenSeq = true
	}
	typ := stringValue(event["type"])
	changed := false
	emit := func(value map[string]any) ([]byte, error) {
		value["sequence_number"] = r.nextSeq
		r.nextSeq++
		return kitutil.Marshal(value)
	}
	switch typ {
	case "response.output_item.added", "response.output_item.done":
		item, _ := event["item"].(map[string]any)
		if call := r.recordItem(event, item); call != nil {
			changed = true
			item["id"] = call.clientItemID
			delete(item, "namespace")
			if call.kind == "custom" {
				item["type"] = "custom_tool_call"
				item["input"] = ""
				delete(item, "arguments")
				if typ == "response.output_item.done" {
					item["input"] = extractCustomToolCallInput(call.arguments.String())
				}
			} else {
				item["type"] = "tool_search_call"
				item["execution"] = "client"
				delete(item, "name")
				item["arguments"] = json.RawMessage(`{}`)
				if typ == "response.output_item.done" {
					item["arguments"] = toolSearchCallArgumentsJSON(call.arguments.String())
				}
			}
			if call.clientItemID == "" {
				delete(item, "id")
			}
			if typ == "response.output_item.done" {
				delete(r.calls, call.itemID)
				delete(r.calls, call.callID)
				delete(r.byOutput, call.outputIndex)
			}
		}
	case "response.function_call_arguments.delta":
		if call := r.callFor(event); call != nil {
			call.arguments.WriteString(stringValue(event["delta"]))
			return nil, true, nil
		}
	case "response.function_call_arguments.done":
		if call := r.callFor(event); call != nil {
			if args := stringValue(event["arguments"]); args != "" {
				call.arguments.Reset()
				call.arguments.WriteString(args)
			}
			if call.kind != "custom" {
				return nil, true, nil
			}
			input := extractCustomToolCallInput(call.arguments.String())
			result := [][]byte{}
			if input != "" {
				delta, err := emit(map[string]any{"type": "response.custom_tool_call_input.delta", "output_index": call.outputIndex, "item_id": call.clientItemID, "delta": input})
				if err != nil {
					return nil, false, err
				}
				result = append(result, delta)
			}
			done, err := emit(map[string]any{"type": "response.custom_tool_call_input.done", "output_index": call.outputIndex, "item_id": call.clientItemID, "call_id": call.callID, "name": call.name, "input": input})
			if err != nil {
				return nil, false, err
			}
			return append(result, done), true, nil
		}
	}
	// This also restores terminal response.output arrays and namespace calls.
	changed = restoreClientToolValue(event, &r.adapter) || changed
	changed = restoreResponsesNamespaceValue(event, r.adapter.NamespaceTools) || changed
	if typ == "response.function_call_arguments.delta" || typ == "response.function_call_arguments.done" {
		if name, ok := r.adapter.NamespaceTools[stringValue(event["name"])]; ok {
			event["name"] = name.Name
			changed = true
		}
	}
	if !changed && r.nextSeq == sequence {
		r.nextSeq++
		return [][]byte{payload}, false, nil
	}
	encoded, err := emit(event)
	if err != nil {
		return nil, false, err
	}
	return [][]byte{encoded}, true, nil
}
func eventIndex(event map[string]any, key string) int {
	number, _ := event[key].(float64)
	return int(number)
}
func (r *ResponsesClientToolStreamRestorer) recordItem(event, item map[string]any) *streamCall {
	if item == nil || stringValue(item["type"]) != "function_call" {
		return nil
	}
	name := stringValue(item["name"])
	kind := ""
	if r.adapter.CustomTools[name] {
		kind = "custom"
	} else if r.adapter.ToolSearch && name == toolSearchProxyName {
		kind = "tool_search"
	}
	if kind == "" {
		return nil
	}
	id, callID := stringValue(item["id"]), stringValue(item["call_id"])
	key := id
	if key == "" {
		key = callID
	}
	call := r.calls[key]
	if call == nil {
		targetType := "tool_search_call"
		if kind == "custom" {
			targetType = "custom_tool_call"
		}
		call = &streamCall{kind: kind, name: name, itemID: id, callID: callID, clientItemID: retypedResponsesToolCallItemID(id, targetType), outputIndex: eventIndex(event, "output_index")}
		r.calls[key] = call
		if callID != "" {
			r.calls[callID] = call
		}
		r.byOutput[call.outputIndex] = call
	}
	if args := stringValue(item["arguments"]); args != "" {
		call.arguments.Reset()
		call.arguments.WriteString(args)
	}
	return call
}
func (r *ResponsesClientToolStreamRestorer) callFor(event map[string]any) *streamCall {
	if call := r.calls[stringValue(event["item_id"])]; call != nil {
		return call
	}
	if call := r.calls[stringValue(event["call_id"])]; call != nil {
		return call
	}
	if call := r.byOutput[eventIndex(event, "output_index")]; call != nil {
		return call
	}
	for _, call := range r.calls {
		if name := stringValue(event["name"]); name != "" && name == call.name {
			return call
		}
	}
	return nil
}
