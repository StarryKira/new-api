package relayconvert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestMarshalResponsePreservesBillingOutOfBand(t *testing.T) {
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, InputTokens: 10, OutputTokens: 2}
	usage.BillingUsage = dto.NewOpenAIChatBillingUsage(usage)
	result := ResponseResult{To: types.RelayFormatOpenAI, Usage: usage, Value: &dto.OpenAITextResponse{Usage: *usage}}
	body, err := MarshalResponse(result)
	require.NoError(t, err)
	require.NotContains(t, string(body), "billing_usage")
	require.NotContains(t, string(body), "input_tokens")
	require.Contains(t, string(body), `"prompt_tokens":10`)
	require.NotNil(t, result.Usage.BillingUsage)
	require.Equal(t, 10, result.Usage.InputTokens)
}

func TestMarshalResponseUnwrapsStreamPayload(t *testing.T) {
	var payload dto.ResponsesStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`), &payload))
	payload.Response.Usage.BillingUsage = dto.NewOpenAIResponsesBillingUsage(payload.Response.Usage)
	body, err := MarshalResponse(ResponseResult{To: types.RelayFormatOpenAIResponses, Stream: true, Value: ChatToResponsesStreamEvent{Type: payload.Type, Payload: payload}})
	require.NoError(t, err)
	var object map[string]json.RawMessage
	require.NoError(t, kitutil.Unmarshal(body, &object))
	require.JSONEq(t, `"response.completed"`, string(object["type"]))
	require.NotContains(t, object, "Payload")
	require.NotContains(t, string(body), "billing_usage")
	require.NotContains(t, string(body), "prompt_tokens")
	require.Contains(t, string(body), `"input_tokens":10`)
}

func TestMarshalResponseDoesNotScrubToolContent(t *testing.T) {
	value := map[string]any{"usage": map[string]any{"input_tokens": 10, "billing_usage": map[string]any{"source": "test"}}, "output": []any{map[string]any{"type": "function_call", "arguments": `{"billing_usage":"user value"}`}}}
	body, err := MarshalResponse(ResponseResult{To: types.RelayFormatOpenAIResponses, Value: value})
	require.NoError(t, err)
	require.Contains(t, string(body), `\"billing_usage\":\"user value\"`)
	require.NotContains(t, string(body), `"source":"test"`)
}

func TestResponsesWireRequiredFieldsAndUnknownPartContent(t *testing.T) {
	for _, raw := range []string{
		`{"type":"response.output_text.delta","delta":"hi"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1"}}`,
		`{"type":"response.content_part.done","part":{"type":"output_text","text":"hi","annotations":[{"type":"url_citation","url":"https://example.com"}],"future_field":true}}`,
		`{"type":"response.content_part.done","part":{"type":"refusal","refusal":"blocked"}}`,
	} {
		out, err := NormalizeResponsesStreamEventJSON([]byte(raw))
		require.NoError(t, err)
		var event map[string]json.RawMessage
		require.NoError(t, kitutil.Unmarshal(out, &event))
		require.JSONEq(t, "0", string(event["sequence_number"]))
		require.JSONEq(t, "0", string(event["output_index"]))
		if item := event["item"]; len(item) > 0 {
			require.Contains(t, string(item), `"arguments":""`)
			require.Contains(t, string(item), `"call_id":""`)
		}
		if strings.Contains(raw, "future_field") {
			require.Contains(t, string(out), `"future_field":true`)
			require.Contains(t, string(out), `"url_citation"`)
		}
		if strings.Contains(raw, "refusal") {
			require.Contains(t, string(out), `"refusal":"blocked"`)
			require.NotContains(t, string(out), `"text"`)
		}
	}
}

func TestResponsesUsageWireKeepsCacheWritesWithoutLegacyFields(t *testing.T) {
	for _, input := range []string{
		`{"input_tokens":22,"output_tokens":2,"cache_creation_input_tokens":4}`,
		`{"input_tokens":22,"output_tokens":2,"input_tokens_details":{"cache_write_tokens":4},"billing_usage":{"source":"upstream"}}`,
	} {
		out, err := FormatResponsesUsageJSON([]byte(input))
		require.NoError(t, err)
		require.NotContains(t, string(out), "cache_creation_input_tokens")
		require.NotContains(t, string(out), "billing_usage")
		require.Contains(t, string(out), `"cached_creation_tokens":4`)
		require.Contains(t, string(out), `"input_tokens":22`)
	}
}
