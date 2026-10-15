package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestRequireTerminalEventRejectsTruncatedStreamsAcrossRoutes(t *testing.T) {
	ctx := context.Background()
	formats := []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini}
	cases := []struct {
		from              types.RelayFormat
		partial, terminal any
	}{
		{types.RelayFormatOpenAI,
			compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"delta":{"content":"partial"}}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`),
			compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)},
		{types.RelayFormatOpenAIResponses,
			compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.created","response":{"id":"r","usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}}`),
			compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}`)},
		{types.RelayFormatClaude,
			compatDecode[dto.ClaudeResponse](t, `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","usage":{"input_tokens":4,"output_tokens":1}}}`),
			compatDecode[dto.ClaudeResponse](t, `{"type":"message_stop"}`)},
		{types.RelayFormatGemini,
			compatDecode[dto.GeminiChatResponse](t, `{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":1,"totalTokenCount":5}}`),
			compatDecode[dto.GeminiChatResponse](t, `{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`)},
	}
	for _, tc := range cases {
		for _, target := range formats {
			t.Run(string(tc.from)+"_to_"+string(target), func(t *testing.T) {
				for _, complete := range []bool{false, true} {
					state, err := NewResponseStreamState(tc.from, target, ResponseStreamOptions{RequireTerminalEvent: true})
					require.NoError(t, err)
					_, err = ConvertStreamResponseChunk(ctx, nil, state, tc.partial)
					require.NoError(t, err)
					if complete {
						_, err = ConvertStreamResponseChunk(ctx, nil, state, tc.terminal)
						require.NoError(t, err)
					}
					output, err := FinalizeStreamResponse(ctx, nil, state)
					if complete {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, ErrIncompleteResponseStream)
						require.Empty(t, output, "a truncated stream must not synthesize successful completion")
						require.NotNil(t, state.Usage())
						require.Equal(t, 4, state.Usage().PromptTokens)
						state.MarkTransportComplete()
						_, retryErr := FinalizeStreamResponse(ctx, nil, state)
						require.ErrorIs(t, retryErr, ErrIncompleteResponseStream, "failure stays terminal")
					}
				}
			})
		}
	}
}

func TestRequireTerminalEventAcceptsTransportTerminatorAndDefaultsOff(t *testing.T) {
	for _, mode := range []string{"legacy", "explicit_done", "strict_eof"} {
		state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{RequireTerminalEvent: mode != "legacy"})
		require.NoError(t, err)
		if mode == "explicit_done" {
			state.MarkTransportComplete()
		}
		results, err := FinalizeStreamResponse(context.Background(), nil, state)
		if mode == "strict_eof" {
			require.ErrorIs(t, err, ErrIncompleteResponseStream)
			require.Empty(t, results)
		} else {
			require.NoError(t, err)
			require.NotEmpty(t, results)
		}
	}
}

func TestSourceTerminalSignals(t *testing.T) {
	for _, raw := range []string{`{"type":"message_stop"}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`} {
		require.True(t, sourceStreamHasTerminal(compatDecode[dto.ClaudeResponse](t, raw)))
	}
	require.False(t, sourceStreamHasTerminal(compatDecode[dto.ClaudeResponse](t, `{"type":"message_delta","delta":{"stop_reason":""}}`)))
	require.False(t, sourceStreamHasTerminal(compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"finish_reason":""}]}`)))
	for _, kind := range []string{"response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled"} {
		require.True(t, sourceStreamHasTerminal(dto.ResponsesStreamResponse{Type: kind}))
	}
	require.True(t, sourceStreamHasTerminal(compatDecode[dto.GeminiChatResponse](t, `{"promptFeedback":{"blockReason":"SAFETY"}}`)))
	require.False(t, sourceStreamHasTerminal(compatDecode[dto.GeminiChatResponse](t, `{"promptFeedback":{"blockReason":""}}`)))
}

func TestFailedStreamKeepsUsageAndCannotFinalizeSuccess(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatClaude, ResponseStreamOptions{RequireTerminalEvent: true})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.failed","response":{"status":"failed","error":{"code":"upstream_error","message":"interrupted"},"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10}}}`))
	require.Error(t, err)
	require.NotNil(t, state.Usage())
	require.Equal(t, 7, state.Usage().PromptTokens)
	require.Equal(t, 3, state.Usage().CompletionTokens)
	final, finalErr := FinalizeStreamResponse(context.Background(), nil, state)
	require.Error(t, finalErr)
	require.Empty(t, final)
}

func TestClaudeAccumulatorRequiresTerminalEvent(t *testing.T) {
	for _, terminal := range []string{"", `{"type":"message_stop"}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`} {
		acc := NewClaudeBufferedAccumulator()
		require.NoError(t, acc.ProcessEventJSON([]byte(`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[]}}`)))
		if terminal != "" {
			require.NoError(t, acc.ProcessEventJSON([]byte(terminal)))
		}
		output, err := acc.ResponseJSON()
		if terminal == "" {
			require.ErrorIs(t, err, ErrIncompleteResponseStream)
			require.Empty(t, output)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestStrictUsageOnlyChunkDoesNotCompleteClaudeStream(t *testing.T) {
	for _, done := range []bool{false, true} {
		state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatClaude, ResponseStreamOptions{RequireTerminalEvent: true})
		require.NoError(t, err)
		for _, raw := range []string{
			`{"choices":[{"delta":{"content":"partial"}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`,
		} {
			results, err := ConvertStreamResponseChunk(context.Background(), nil, state, compatDecode[dto.ChatCompletionsStreamResponse](t, raw))
			require.NoError(t, err)
			for _, result := range results {
				require.NotEqual(t, "message_stop", result.Value.(*dto.ClaudeResponse).Type)
			}
		}
		if done {
			state.MarkTransportComplete()
		}
		final, err := FinalizeStreamResponse(context.Background(), nil, state)
		if done {
			require.NoError(t, err)
			require.Equal(t, "message_stop", final[len(final)-1].Value.(*dto.ClaudeResponse).Type)
		} else {
			require.ErrorIs(t, err, ErrIncompleteResponseStream)
			require.Empty(t, final)
		}
		require.Equal(t, 4, state.Usage().PromptTokens)
		require.Equal(t, 1, state.Usage().CompletionTokens)
	}
}
