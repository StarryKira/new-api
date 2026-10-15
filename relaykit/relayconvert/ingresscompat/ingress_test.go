package ingresscompat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/responsescompat"
	"github.com/stretchr/testify/require"
)

func TestLegacyIngressPreservesNativeInputAndUnknownNumbers(t *testing.T) {
	body := []byte(`{"input":"native","messages":[{"role":"user","content":"legacy"}],"future_counter":9007199254740993,"prompt":{"id":"pmpt_1"}}`)
	out, changed, err := NormalizeLegacyIngress(context.Background(), nil, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(out), `"input":"native"`)
	require.Contains(t, string(out), `9007199254740993`)
	require.Contains(t, string(out), `"prompt":{"id":"pmpt_1"}`)
	require.NotContains(t, string(out), `"messages"`)
}

func TestLegacyIngressUsesCoreToolAndReasoningConversion(t *testing.T) {
	body := []byte(`{"model":"test","messages":[{"role":"user","content":"hello"}],"functions":[{"name":"lookup","parameters":{"type":"object"}}],"function_call":{"name":"lookup"},"reasoning_effort":"high","service_tier":"priority"}`)
	out, changed, err := NormalizeLegacyIngress(context.Background(), nil, body)
	require.NoError(t, err)
	require.True(t, changed)
	var request map[string]json.RawMessage
	require.NoError(t, kitutil.Unmarshal(out, &request))
	require.JSONEq(t, `{"type":"function","name":"lookup"}`, string(request["tool_choice"]))
	require.Contains(t, string(request["tools"]), `"name":"lookup"`)
	require.Contains(t, string(request["reasoning"]), `"effort":"high"`)
	require.JSONEq(t, `"priority"`, string(request["service_tier"]))
	require.NotContains(t, request, "functions")
}

func TestGrokCompactionRoundTripKeepsOpaqueReplayAndSummary(t *testing.T) {
	response, err := responsescompat.GrokResponseToCompaction([]byte(`{"id":"resp_1","usage":{"input_tokens":5},"output":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","content":[{"type":"output_text","text":"summary"}]}]}`), "cmp_test")
	require.NoError(t, err)
	var payload map[string]json.RawMessage
	require.NoError(t, kitutil.Unmarshal(response, &payload))
	input, err := kitutil.Marshal(map[string]json.RawMessage{"input": payload["output"]})
	require.NoError(t, err)
	restored, err := responsescompat.RestoreGrokCompactionInput(input)
	require.NoError(t, err)
	require.Contains(t, string(restored), `"type":"reasoning"`)
	require.Contains(t, string(restored), `"encrypted_content":"opaque"`)
	require.Contains(t, string(restored), `summary`)
	require.JSONEq(t, `{"input_tokens":5}`, string(payload["usage"]))
	_, err = responsescompat.GrokResponseToCompaction([]byte(`{"output":[]}`), "cmp_empty")
	require.Error(t, err)
}
