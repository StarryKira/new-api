package claude

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func FunctionParametersToInputSchema(parameters any) map[string]any {
	params, _ := parameters.(map[string]any)
	schema := make(map[string]any, len(params)+2)
	for key, value := range params {
		schema[key] = value
	}
	if schema["type"] == nil {
		schema["type"] = "object"
	}
	if schema["properties"] == nil {
		schema["properties"] = map[string]any{}
	}
	if schema["oneOf"] == nil && schema["anyOf"] == nil && schema["allOf"] == nil {
		return schema
	}
	data, _ := kitutil.Marshal(schema)
	var raw map[string]json.RawMessage
	if kitutil.Unmarshal(data, &raw) == nil {
		flattenAnthropicRootUnions(raw)
		data, _ = kitutil.Marshal(raw)
		schema = nil
		_ = kitutil.Unmarshal(data, &schema)
	}
	return schema
}
