package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestValidateResponseJSONRejectsErrorEnvelopes(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			for _, raw := range []string{`{"error":{"message":"upstream failed"}}`, `{"error":"failed"}`, `{"error":{}}`, `null`, `[]`, `{broken`} {
				require.Error(t, ValidateResponseJSON(format, []byte(raw)), raw)
			}
			require.NoError(t, ValidateResponseJSON(format, []byte(`{"error":null}`)))
			require.NoError(t, ValidateResponseJSON(format, []byte(`{"candidates":[{"content":{"parts":[{"text":"error: a word in normal output"}]}}]}`)))
		})
	}
}
