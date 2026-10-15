package responsescompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A function-only upstream answers a lowered custom tool with an fc_ item ID.
// Emitting that ID on the restored custom_tool_call breaks the client the next
// time the item is replayed to an upstream that validates item IDs:
// "Invalid 'input[N].id': 'fc_...'. Expected an ID that begins with 'ctc'".
func TestRestoreResponsesClientToolPayload_RetypesToolCallItemIDs(t *testing.T) {
	mapping := ResponsesClientToolMapping{
		CustomTools: map[string]bool{"exec": true}, ToolSearch: true,
		NamespaceTools: map[string]ResponsesNamespaceName{"team__send": {Namespace: "team", Name: "send"}},
	}
	payload := []byte(`{"id":"resp","output":[` +
		`{"type":"function_call","id":"fc_abc123","call_id":"call_1","name":"exec","arguments":"{\"input\":\"dir\"}"},` +
		`{"type":"function_call","id":"fc_def456","call_id":"call_2","name":"tool_search","arguments":"{\"query\":\"git\"}"},` +
		`{"type":"function_call","id":"fc_ghi789","call_id":"call_3","name":"team__send","arguments":"{}"}]}`)

	restored, changed, err := RestoreResponsesClientToolPayload(payload, mapping)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"id":"resp","output":[`+
		`{"type":"custom_tool_call","id":"ctc_abc123","call_id":"call_1","name":"exec","input":"dir"},`+
		`{"type":"tool_search_call","id":"tsc_def456","call_id":"call_2","execution":"client","arguments":{"query":"git"}},`+
		`{"type":"function_call","id":"fc_ghi789","call_id":"call_3","name":"send","namespace":"team","arguments":"{}"}]}`,
		string(restored))
}

func TestAdaptResponsesClientTools_RecoversRetypedToolCallItemID(t *testing.T) {
	req := map[string]any{
		"tools": []any{
			map[string]any{"type": "custom", "name": "exec"},
			map[string]any{"type": "tool_search"},
		},
		"input": []any{
			map[string]any{"type": "custom_tool_call", "id": "ctc_upstream1", "call_id": "call_1", "name": "exec", "input": "dir"},
			map[string]any{"type": "tool_search_call", "id": "tsc_upstream2", "call_id": "call_2", "arguments": map[string]any{"query": "git"}},
			map[string]any{"type": "custom_tool_call_output", "id": "ctco_client", "call_id": "call_1", "output": "ok"},
		},
	}

	_, changed, err := AdaptResponsesClientTools(req)
	require.NoError(t, err)
	require.True(t, changed)

	input := requireResponsesClientToolValue[[]any](t, req["input"])
	require.Len(t, input, 3)
	customCall := requireResponsesClientToolValue[map[string]any](t, input[0])
	require.Equal(t, "function_call", customCall["type"])
	require.Equal(t, "fc_upstream1", customCall["id"])
	searchCall := requireResponsesClientToolValue[map[string]any](t, input[1])
	require.Equal(t, "function_call", searchCall["type"])
	require.Equal(t, "fc_upstream2", searchCall["id"])
	// Output items have no function-protocol ID counterpart and stay dropped.
	customOutput := requireResponsesClientToolValue[map[string]any](t, input[2])
	require.Equal(t, "function_call_output", customOutput["type"])
	require.NotContains(t, customOutput, "id")
}
