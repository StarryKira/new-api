package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestClaudeAccumulatorPreservesStartArgumentsAndSignedThinking(t *testing.T) {
	a := NewClaudeBufferedAccumulator()
	for _, raw := range []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","future_field":true,"usage":{"input_tokens":10,"cache_read_input_tokens":20}}}`,
		`{"type":"content_block_start","index":-1,"content_block":{"type":"text","text":"invalid"}}`,
		`{"type":"content_block_delta","index":-1,"delta":{"type":"text_delta","text":"invalid"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"reason","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"run","input":{"seed":true}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
	} {
		require.NoError(t, a.ProcessEventJSON([]byte(raw)))
	}
	raw, err := a.ResponseJSON()
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, kitutil.Unmarshal(raw, &got))
	require.Equal(t, true, got["future_field"])
	require.Equal(t, float64(20), got["usage"].(map[string]any)["cache_read_input_tokens"])
	blocks := got["content"].([]any)
	require.Len(t, blocks, 2)
	require.Equal(t, "signed", blocks[0].(map[string]any)["signature"])
	require.Equal(t, map[string]any{"seed": true}, blocks[1].(map[string]any)["input"])
	require.NoError(t, a.ProcessEventJSON([]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"actual\":"}}`)))
	require.NoError(t, a.ProcessEventJSON([]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"true}"}}`)))
	raw, err = a.ResponseJSON()
	require.NoError(t, err)
	require.Contains(t, string(raw), `"input":{"actual":true}`)
	require.NotContains(t, string(raw), `"seed"`)
}

func TestClaudeAccumulatorRejectsInvalidArgumentsAndUpstreamErrors(t *testing.T) {
	a := NewClaudeBufferedAccumulator()
	require.NoError(t, a.ProcessEventJSON([]byte(`{"type":"message_start","message":{"type":"message"}}`)))
	require.NoError(t, a.ProcessEventJSON([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","input":{}}}`)))
	require.NoError(t, a.ProcessEventJSON([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{"}}`)))
	require.NoError(t, a.ProcessEventJSON([]byte(`{"type":"message_stop"}`)))
	_, err := a.ResponseJSON()
	require.ErrorContains(t, err, "invalid Claude tool arguments")
	require.Error(t, a.ProcessEventJSON([]byte(`{"type":"error","error":{"type":"overloaded_error"}}`)))
	_, err = a.ResponseJSON()
	require.Error(t, err)
}
