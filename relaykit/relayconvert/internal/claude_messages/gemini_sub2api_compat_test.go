package claudemessages

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestClaudeGeminiPreservesBlockOrderAndIndividualSignatures(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-2.5-flash","messages":[{"role":"assistant","content":[{"type":"text","text":"before"},{"type":"thinking","thinking":"first","signature":"sig-first"},{"type":"tool_use","id":"tool-1","name":"run","input":{"x":1},"signature":"sig-tool"},{"type":"thinking","thinking":"second","signature":"sig-second"},{"type":"text","text":"after"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}]}`), &request))
	result, err := ClaudeMessagesRequestToGemini(context.Background(), request, &convmeta.Values{UpstreamModelName: request.Model, ChannelMetaAttached: true})
	require.NoError(t, err)
	require.Len(t, result.Contents, 2)
	parts := result.Contents[0].Parts
	require.Len(t, parts, 5)
	require.Equal(t, "before", parts[0].Text)
	require.True(t, parts[1].Thought)
	require.JSONEq(t, `"sig-first"`, string(parts[1].ThoughtSignature))
	require.Equal(t, "tool-1", parts[2].FunctionCall.ID)
	require.JSONEq(t, `"sig-tool"`, string(parts[2].ThoughtSignature))
	require.True(t, parts[3].Thought)
	require.JSONEq(t, `"sig-second"`, string(parts[3].ThoughtSignature))
	require.Equal(t, "after", parts[4].Text)
	resultPart := result.Contents[1].Parts[0].FunctionResponse
	require.Equal(t, "run", resultPart.Name)
	require.JSONEq(t, `"tool-1"`, string(resultPart.ID))
}

func TestClaudeGeminiBudgetOnlyGateway(t *testing.T) {
	for _, test := range []struct {
		name, thinking string
		budget         int
	}{
		{"dynamic", `{"type":"enabled"}`, -1},
		{"explicit", `{"type":"enabled","budget_tokens":2048}`, 2048},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request dto.ClaudeRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"claude-sonnet-4-5","max_tokens":64000,"thinking":`+test.thinking+`}`), &request))
			options := &convmeta.Options{}
			options.Gemini.BudgetOnlyThinking = true
			result, err := ClaudeMessagesRequestToGemini(context.Background(), request, &convmeta.Values{UpstreamModelName: request.Model, ChannelMetaAttached: true, Options: options})
			require.NoError(t, err)
			require.NotNil(t, result.GenerationConfig.ThinkingConfig)
			require.Equal(t, test.budget, *result.GenerationConfig.ThinkingConfig.ThinkingBudget)
			require.True(t, *result.GenerationConfig.ThinkingConfig.IncludeThoughts)
		})
	}
}
