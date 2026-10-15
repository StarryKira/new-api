package responsescompat

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRawCustomStreamPreservesFieldsAndRetypesIDs(t *testing.T) {
	restorer := NewResponsesClientToolStreamRestorer(ResponsesClientToolMapping{CustomTools: map[string]bool{"exec": true}})
	added, _, err := restorer.RestoreEvent([]byte(`{"type":"response.output_item.added","sequence_number":7,"output_index":0,"vendor":"keep","item":{"type":"function_call","id":"fc_1","call_id":"c","name":"exec","arguments":"","vendor_item":"keep"}}`))
	require.NoError(t, err)
	require.Len(t, added, 1)
	require.Equal(t, "ctc_1", gjson.GetBytes(added[0], "item.id").String())
	require.Equal(t, "keep", gjson.GetBytes(added[0], "vendor").String())
	require.Equal(t, "keep", gjson.GetBytes(added[0], "item.vendor_item").String())
	delta, _, err := restorer.RestoreEvent([]byte(`{"type":"response.function_call_arguments.delta","sequence_number":8,"output_index":0,"item_id":"fc_1","delta":"{\"input\":\"ls\"}"}`))
	require.NoError(t, err)
	require.Empty(t, delta)
	done, _, err := restorer.RestoreEvent([]byte(`{"type":"response.function_call_arguments.done","sequence_number":9,"output_index":0,"item_id":"fc_1"}`))
	require.NoError(t, err)
	require.Len(t, done, 2)
	require.Equal(t, "ls", gjson.GetBytes(done[0], "delta").String())
	require.Equal(t, int64(8), gjson.GetBytes(done[0], "sequence_number").Int())
	require.Equal(t, int64(9), gjson.GetBytes(done[1], "sequence_number").Int())
	item, _, err := restorer.RestoreEvent([]byte(`{"type":"response.output_item.done","sequence_number":10,"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"c","name":"exec","arguments":"{\"input\":\"ls\"}"}}`))
	require.NoError(t, err)
	require.Len(t, item, 1)
	require.Equal(t, "ls", gjson.GetBytes(item[0], "item.input").String())
	require.False(t, gjson.GetBytes(item[0], "item.arguments").Exists())
}

func TestResponsesMetadataInspectorPreservesNativeDeclarations(t *testing.T) {
	tools, err := EffectiveTools([]byte(`[{"type":"custom","name":"exec"}]`), []byte(`[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"wait","parameters":{"const":9007199254740993}}]}]}]`))
	require.NoError(t, err)
	require.Contains(t, string(tools), "9007199254740993")
	metadata := InspectTools(tools)
	require.True(t, metadata.Custom["exec"])
	require.True(t, metadata.Functions["wait"])
	require.Equal(t, ResponsesNamespaceName{Namespace: "functions", Name: "wait"}, metadata.Namespaces["wait"])
	require.Equal(t, metadata.Namespaces["wait"], metadata.Namespaces["functions__wait"])
	id, text, ok := ExtractReasoningItem([]byte(`{"type":"reasoning","id":"rs_1","summary":[{"text":"one"},{"text":"two"}]}`))
	require.True(t, ok)
	require.Equal(t, "rs_1", id)
	require.Equal(t, "one\ntwo", text)
}
