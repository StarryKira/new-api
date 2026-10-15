package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestChatInstructionsExtensionSurvivesEveryTarget(t *testing.T) {
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(target), func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_tokens":128,"instructions":"Return concise JSON","messages":[{"role":"system","content":"Preserve sources"},{"role":"user","content":"Find alpha"}]}`), &request))
			result, err := ConvertRequest(context.Background(), nil, target, &request)
			require.NoError(t, err)
			encoded, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			var fields struct {
				Instructions string `json:"instructions"`
				System       []struct {
					Text string `json:"text"`
				} `json:"system"`
				SystemInstruction *struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"systemInstruction"`
			}
			require.NoError(t, kitutil.Unmarshal(encoded, &fields))
			switch target {
			case types.RelayFormatOpenAIResponses:
				require.Equal(t, "Return concise JSON\n\nPreserve sources", fields.Instructions)
			case types.RelayFormatClaude:
				require.Len(t, fields.System, 1)
				require.Equal(t, "Return concise JSON Preserve sources", fields.System[0].Text)
			case types.RelayFormatGemini:
				require.NotNil(t, fields.SystemInstruction)
				require.Len(t, fields.SystemInstruction.Parts, 1)
				require.Equal(t, "Return concise JSON\nPreserve sources", fields.SystemInstruction.Parts[0].Text)
			}
			require.Contains(t, string(encoded), "Find alpha")
			require.Len(t, request.Messages, 2, "input request is not mutated")
			require.Equal(t, "Preserve sources", request.Messages[0].StringContent())
			require.NotNil(t, request.Instructions)
			require.Equal(t, "Return concise JSON", *request.Instructions)
		})
	}
}
