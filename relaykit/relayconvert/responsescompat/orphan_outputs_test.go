package responsescompat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestDropOrphanToolOutputsPreservesStandaloneAndPairedItems(t *testing.T) {
	const raw = `[
		{"type":"function_call_output","name":"delegate","namespace":"app","output":{"unknown":1e1000}},
		{"type":"function_call_output","call_id":"orphan","output":"discard"},
		{"type":"tool_search_output","call_id":"orphan","output":"also discard"},
		{"type":"custom_tool_call_output","call_id":"referenced","output":"retain"},
		{"type":"tool_search_output","call_id":" search ","output":"retain"},
		{"type":"mcp_tool_call_output","call_id":"mcp","output":"retain"},
		{"type":"function_call_output","name":" ","output":"discard"},
		{"type":"custom_tool_call_output","name":"named_custom","output":"discard"},
		{"type":"tool_search_call","id":"search"},
		{"type":"mcp_tool_call","call_id":"mcp"},
		{"type":"item_reference","id":"referenced"},
		{"type":"future_type","value":true}, "user text", null
	]`
	var input []any
	require.NoError(t, kitutil.UnmarshalUseNumber([]byte(raw), &input))
	before, err := kitutil.Marshal(input)
	require.NoError(t, err)
	out, changed := DropOrphanToolOutputs(input)
	require.True(t, changed)
	require.Equal(t, []any{input[0], input[3], input[4], input[5], input[8], input[9], input[10], input[11], input[12], input[13]}, out)
	after, err := kitutil.Marshal(input)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "must not mutate source items or slice")
	require.Contains(t, string(after), "1e1000", "unknown values remain lossless")
	again, changed := DropOrphanToolOutputs(out)
	require.False(t, changed)
	require.Equal(t, out, again)
}

func TestDropOrphanToolOutputsAllContextTypesAndIDFallback(t *testing.T) {
	for _, kind := range []string{"tool_call", "function_call", "local_shell_call", "tool_search_call", "custom_tool_call", "mcp_tool_call"} {
		for _, key := range []string{"call_id", "id"} {
			input := []any{map[string]any{"type": "function_call_output", "call_id": "matched"}, map[string]any{"type": kind, key: "matched"}}
			got, changed := DropOrphanToolOutputs(input)
			require.False(t, changed)
			require.Equal(t, input, got)
		}
	}
	got, changed := DropOrphanToolOutputs(nil)
	require.Nil(t, got)
	require.False(t, changed)
}
