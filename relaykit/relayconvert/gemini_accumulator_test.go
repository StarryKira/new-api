package relayconvert

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiBufferedAccumulatorRetainsOrderedPartsAndTerminalMetadata(t *testing.T) {
	a := NewGeminiBufferedAccumulator()
	for _, frame := range []string{
		`{"responseId":"source-id","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"before "}]}}]}`,
		`{"candidates":[{"index":0,"content":{"parts":[{"text":"text"},{"text":"thought","thought":true,"thoughtSignature":"sig"},{"functionCall":{"id":"call_1","name":"tool","args":{"x":1}},"thoughtSignature":"tool-sig"},{"inlineData":{"mimeType":"image/png","data":"YQ=="}},{"text":"after","thoughtSignature":"text-sig"}]}}]}`,
		`{"candidates":[{"index":0,"finishReason":"MAX_TOKENS","future":"preserved"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3},"modelVersion":"gemini"}`,
	} {
		require.NoError(t, a.ProcessEventJSON([]byte(frame)))
	}
	data, err := a.ResponseJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"responseId":"source-id","modelVersion":"gemini","usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3},"candidates":[{"index":0,"finishReason":"MAX_TOKENS","future":"preserved","content":{"role":"model","parts":[{"text":"before text"},{"text":"thought","thought":true,"thoughtSignature":"sig"},{"functionCall":{"id":"call_1","name":"tool","args":{"x":1}},"thoughtSignature":"tool-sig"},{"inlineData":{"mimeType":"image/png","data":"YQ=="}},{"text":"after","thoughtSignature":"text-sig"}]}}]}`, string(data))
}

func TestGeminiBufferedAccumulatorRejectsMalformedFrames(t *testing.T) {
	for _, raw := range []string{`{broken`, `null`, `{"error":{"message":"failed"}}`, `{"unrelated":"event"}`, `{"candidates":[null]}`, `{"candidates":[{"index":-1}]}`, `{"candidates":[{"content":{"parts":[null]}}]}`} {
		a := NewGeminiBufferedAccumulator()
		require.Error(t, a.ProcessEventJSON([]byte(raw)), raw)
		_, err := a.ResponseJSON()
		require.Error(t, err, raw)
	}
}
