package relayconvert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// NormalizeResponsesStreamEventJSON preserves unknown event fields and renders
// required zero indices and empty content/tool values for strict SDK decoders.
func NormalizeResponsesStreamEventJSON(data []byte) ([]byte, error) {
	var event map[string]json.RawMessage
	if err := kitutil.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	if event == nil {
		return nil, fmt.Errorf("Responses event must be an object")
	}
	var kind string
	if err := kitutil.Unmarshal(event["type"], &kind); err != nil {
		return nil, err
	}
	defaults := func(object map[string]json.RawMessage, key, value string) {
		if raw := object[key]; len(raw) == 0 || string(raw) == "null" {
			object[key] = json.RawMessage(value)
		}
	}
	if strings.HasPrefix(kind, "response.") {
		defaults(event, "sequence_number", "0")
	}
	partType := ""
	switch kind {
	case "response.output_text.delta", "response.output_text.done", "response.content_part.added", "response.content_part.done":
		defaults(event, "output_index", "0")
		defaults(event, "content_index", "0")
		if kind == "response.output_text.delta" {
			defaults(event, "delta", `""`)
		}
		if kind == "response.output_text.done" {
			defaults(event, "text", `""`)
		}
		if kind == "response.content_part.added" || kind == "response.content_part.done" {
			partType = "output_text"
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		defaults(event, "output_index", "0")
		defaults(event, "summary_index", "0")
		if kind == "response.reasoning_summary_text.delta" {
			defaults(event, "delta", `""`)
		}
		if kind == "response.reasoning_summary_text.done" {
			defaults(event, "text", `""`)
		}
		if kind == "response.reasoning_summary_part.added" || kind == "response.reasoning_summary_part.done" {
			partType = "summary_text"
		}
	case "response.function_call_arguments.delta", "response.function_call_arguments.done", "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
		defaults(event, "output_index", "0")
		switch kind {
		case "response.function_call_arguments.done":
			defaults(event, "arguments", `""`)
		case "response.custom_tool_call_input.done":
			defaults(event, "input", `""`)
		default:
			defaults(event, "delta", `""`)
		}
	case "response.output_item.added", "response.output_item.done":
		defaults(event, "output_index", "0")
		var item map[string]json.RawMessage
		if raw := event["item"]; len(raw) > 0 && string(raw) != "null" {
			if err := kitutil.Unmarshal(raw, &item); err != nil {
				return nil, err
			}
			var itemType string
			if err := kitutil.Unmarshal(item["type"], &itemType); err != nil {
				return nil, err
			}
			switch itemType {
			case "message":
				defaults(item, "role", `"assistant"`)
				defaults(item, "content", `[]`)
			case "reasoning":
				defaults(item, "summary", `[]`)
			case "function_call":
				defaults(item, "call_id", `""`)
				defaults(item, "name", `""`)
				defaults(item, "arguments", `""`)
			case "custom_tool_call":
				defaults(item, "call_id", `""`)
				defaults(item, "name", `""`)
				defaults(item, "input", `""`)
			case "tool_search_call":
				defaults(item, "call_id", `""`)
				defaults(item, "execution", `"client"`)
				defaults(item, "arguments", `{}`)
				var arguments string
				if kitutil.Unmarshal(item["arguments"], &arguments) == nil {
					if !kitutil.Valid([]byte(arguments)) {
						return nil, fmt.Errorf("invalid tool_search_call arguments")
					}
					item["arguments"] = json.RawMessage(arguments)
				}
			}
			encoded, err := kitutil.Marshal(item)
			if err != nil {
				return nil, err
			}
			event["item"] = encoded
		}
	}
	if partType != "" {
		part := make(map[string]json.RawMessage)
		if raw := event["part"]; len(raw) > 0 {
			if err := kitutil.Unmarshal(raw, &part); err != nil {
				return nil, err
			}
		}
		if part == nil {
			part = make(map[string]json.RawMessage)
		}
		partTypeJSON, _ := kitutil.Marshal(partType)
		defaults(part, "type", string(partTypeJSON))
		var actualType string
		if err := kitutil.Unmarshal(part["type"], &actualType); err != nil {
			return nil, err
		}
		if actualType == "output_text" || actualType == "summary_text" {
			defaults(part, "text", `""`)
		}
		if actualType == "output_text" {
			defaults(part, "annotations", `[]`)
			defaults(part, "logprobs", `[]`)
		}
		encoded, err := kitutil.Marshal(part)
		if err != nil {
			return nil, err
		}
		event["part"] = encoded
	}
	return kitutil.Marshal(event)
}
