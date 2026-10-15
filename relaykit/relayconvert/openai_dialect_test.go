package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestOptedInOpenAIDialectToolsPreserveNativeFieldsAndChoices(t *testing.T) {
	for _, source := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
		for _, choice := range []string{`{"type":"x_search"}`, `"x_search"`} {
			var request any
			target := types.RelayFormat(types.RelayFormatOpenAIResponses)
			raw := `{"model":"grok-4.5","max_tokens":1024,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"x_search","allowed_x_handles":["xai"],"enable_image_understanding":false,"future_field":9007199254740993}],"tool_choice":` + choice + `}`
			if source == types.RelayFormatOpenAI {
				request = &dto.GeneralOpenAIRequest{}
			} else {
				target = types.RelayFormatOpenAI
				request = &dto.OpenAIResponsesRequest{}
				raw = `{"model":"grok-4.5","input":"search","tools":[{"type":"x_search","allowed_x_handles":["xai"],"enable_image_understanding":false,"future_field":9007199254740993}],"tool_choice":` + choice + `}`
			}
			require.NoError(t, kitutil.Unmarshal([]byte(raw), request))
			meta := &convmeta.Values{Options: &convmeta.Options{OpenAICompatibleToolTypes: []string{"x_search"}}}
			result, err := ConvertRequest(context.Background(), meta, target, request)
			require.NoError(t, err)
			encoded, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"allowed_x_handles":["xai"]`)
			require.Contains(t, string(encoded), `"enable_image_understanding":false`)
			require.Contains(t, string(encoded), `9007199254740993`)
			require.Contains(t, string(encoded), `"tool_choice":`+choice)
		}
	}
}
