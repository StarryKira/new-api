package relayconvert

import (
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func compatDecode[T any](t *testing.T, data string) *T {
	t.Helper()
	var value T
	require.NoError(t, kitutil.Unmarshal([]byte(data), &value))
	return &value
}

func compatStream(t *testing.T, from, to types.RelayFormat, inputs []string) ([]any, *ResponseStreamState) {
	t.Helper()
	state, err := NewResponseStreamState(from, to, ResponseStreamOptions{ID: "resp_compat", Model: "claude-opus-5-5", IncludeUsage: true, EmitSequenceNumber: true})
	require.NoError(t, err)
	var values []any
	for _, raw := range inputs {
		var input any
		switch from {
		case types.RelayFormatClaude:
			input = compatDecode[dto.ClaudeResponse](t, raw)
		case types.RelayFormatOpenAI:
			input = compatDecode[dto.ChatCompletionsStreamResponse](t, raw)
		case types.RelayFormatOpenAIResponses:
			input = compatDecode[dto.ResponsesStreamResponse](t, raw)
		}
		results, err := ConvertStreamResponseChunk(context.Background(), nil, state, input)
		require.NoError(t, err)
		for _, result := range results {
			values = append(values, result.Value)
		}
	}
	results, err := FinalizeStreamResponse(context.Background(), nil, state)
	require.NoError(t, err)
	for _, result := range results {
		values = append(values, result.Value)
	}
	return values, state
}

func compatResponsesEvent(t *testing.T, value any) dto.ResponsesStreamResponse {
	t.Helper()
	switch event := value.(type) {
	case ChatToResponsesStreamEvent:
		return event.Payload
	case dto.ResponsesStreamResponse:
		return event
	case *dto.ResponsesStreamResponse:
		return *event
	default:
		t.Fatalf("unexpected event %T", value)
		return dto.ResponsesStreamResponse{}
	}
}

func TestCompatThinkingRequiresSignatureKey(t *testing.T) {
	for _, block := range []dto.ClaudeMediaMessage{{Type: "thinking"}, {Type: "thinking", Thinking: func() *string { s := "plan"; return &s }()}} {
		encoded, err := kitutil.Marshal(block)
		require.NoError(t, err)
		var wire map[string]any
		require.NoError(t, kitutil.Unmarshal(encoded, &wire))
		require.Contains(t, wire, "thinking")
		require.Contains(t, wire, "signature")
		require.Equal(t, "", wire["signature"])
	}
}

func TestCompatClaudeSignedThinkingRoundTripAndOrder(t *testing.T) {
	for _, model := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"} {
		t.Run(model, func(t *testing.T) {
			source := compatDecode[dto.ClaudeResponse](t, `{"id":"msg_signed","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"signed"},{"type":"text","text":"before"},{"type":"redacted_thinking","data":"opaque"},{"type":"text","text":"after"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":4}}`)
			source.Model = model
			result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, source)
			require.NoError(t, err)
			response := result.Value.(*dto.OpenAIResponsesResponse)
			require.Len(t, result.Steps, 1)
			require.Len(t, response.Output, 4)
			require.Equal(t, []string{"reasoning", "message", "reasoning", "message"}, []string{response.Output[0].Type, response.Output[1].Type, response.Output[2].Type, response.Output[3].Type})
			block, ok := sharedclaude.DecodeThinkingBlock(response.Output[0].EncryptedContent)
			require.True(t, ok)
			require.Equal(t, "signed", block.Signature)
			back, err := ConvertResponse(context.Background(), nil, types.RelayFormatClaude, response)
			require.NoError(t, err)
			require.Equal(t, source.Content, back.Value.(*dto.ClaudeResponse).Content)
		})
	}
}

func TestCompatClaudeStreamSignedThinkingAndInlineToolInput(t *testing.T) {
	for _, deltas := range []bool{false, true} {
		t.Run(map[bool]string{false: "start only", true: "delta supersedes seed"}[deltas], func(t *testing.T) {
			inputs := []string{
				`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":7,"cache_creation_input_tokens":3,"cache_read_input_tokens":2}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_1","name":"shell","input":{"cmd":"ls"}}}`,
			}
			want := `{"cmd":"ls"}`
			if deltas {
				inputs = append(inputs, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"pwd\"}"}}`)
				want = `{"cmd":"pwd"}`
			}
			inputs = append(inputs, `{"type":"content_block_stop","index":1}`, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`, `{"type":"message_stop"}`)
			values, state := compatStream(t, types.RelayFormatClaude, types.RelayFormatOpenAIResponses, inputs)
			var argumentDeltas strings.Builder
			var terminal *dto.OpenAIResponsesResponse
			for _, value := range values {
				event := compatResponsesEvent(t, value)
				if event.Type == "response.function_call_arguments.delta" {
					argumentDeltas.WriteString(event.Delta)
				}
				if event.Type == "response.completed" {
					terminal = event.Response
				}
			}
			require.NotNil(t, terminal)
			require.Len(t, terminal.Output, 2)
			require.Equal(t, want, argumentDeltas.String())
			require.Equal(t, want, terminal.Output[1].ArgumentsString())
			native, ok := sharedclaude.DecodeThinkingBlock(terminal.Output[0].EncryptedContent)
			require.True(t, ok)
			require.Equal(t, "signed", native.Signature)
			require.Equal(t, "plan", *native.Thinking)
			require.NotNil(t, state.Usage())
			require.Equal(t, 12, state.Usage().InputTokens)
			require.Equal(t, 5, state.Usage().OutputTokens)
		})
	}
}

func TestCompatChatSingleChunkArgumentsAndLateUsageTier(t *testing.T) {
	values, state := compatStream(t, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, []string{
		`{"id":"chat_1","service_tier":"priority","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shell","arguments":"{\"cmd\":\"ls\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"chat_1","service_tier":"default","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11}}`,
	})
	var args strings.Builder
	for _, value := range values {
		event := compatResponsesEvent(t, value)
		if event.Type == "response.function_call_arguments.delta" {
			args.WriteString(event.Delta)
		}
		if event.Type == "response.completed" {
			require.Equal(t, "default", event.Response.ServiceTier)
			require.Equal(t, `{"cmd":"ls"}`, event.Response.Output[0].ArgumentsString())
			require.Equal(t, 9, event.Response.Usage.InputTokens)
		}
	}
	require.Equal(t, `{"cmd":"ls"}`, args.String())
	require.Equal(t, 9, state.Usage().InputTokens)
}

func TestCompatResponsesEncryptedContentAndTextParts(t *testing.T) {
	values, _ := compatStream(t, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, []string{
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"r_1","type":"reasoning","encrypted_content":"opaque-xai"}}`,
		`{"type":"response.output_text.delta","output_index":1,"content_index":0,"item_id":"m_1","delta":"hel"}`,
		`{"type":"response.output_text.done","output_index":9,"content_index":0,"item_id":"unknown","text":"hello"}`,
		`{"type":"response.output_text.done","output_index":1,"content_index":0,"item_id":"m_1","text":"hello"}`,
		`{"type":"response.output_text.delta","output_index":1,"content_index":1,"item_id":"m_1","delta":" world"}`,
		`{"type":"response.output_item.done","output_index":1,"item":{"id":"m_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_text","text":" world"}]}}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"id":"r_1","type":"reasoning","encrypted_content":"opaque-xai"},{"id":"m_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_text","text":" world"}]}]}}`,
		`{"type":"response.output_text.done","output_index":1,"content_index":0,"text":"hello again"}`,
	})
	var text, signature strings.Builder
	for _, value := range values {
		response := value.(*dto.ClaudeResponse)
		if response.Delta != nil {
			if response.Delta.Text != nil {
				text.WriteString(*response.Delta.Text)
			}
			signature.WriteString(response.Delta.Signature)
		}
	}
	require.Equal(t, "hello world", text.String())
	require.Equal(t, "opaque-xai", signature.String())
}

func TestCompatResponseServiceTierAndCacheConservation(t *testing.T) {
	source := compatDecode[dto.OpenAIResponsesResponse](t, `{"id":"resp_1","service_tier":"default","output":[],"usage":{"input_tokens":100,"output_tokens":5,"total_tokens":105,"input_tokens_details":{"cached_tokens":20,"cached_creation_tokens":30}}}`)
	result, err := ConvertResponse(context.Background(), &convmeta.Values{}, types.RelayFormatOpenAI, source)
	require.NoError(t, err)
	require.Equal(t, "default", result.Value.(*dto.OpenAITextResponse).ServiceTier)
	back, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, result.Value)
	require.NoError(t, err)
	require.Equal(t, "default", back.Value.(*dto.OpenAIResponsesResponse).ServiceTier)
	claude, err := ConvertResponse(context.Background(), nil, types.RelayFormatClaude, source)
	require.NoError(t, err)
	usage := claude.Value.(*dto.ClaudeResponse).Usage
	require.Equal(t, 50, usage.InputTokens)
	require.Equal(t, 20, usage.CacheReadInputTokens)
	require.Equal(t, 30, usage.CacheCreationInputTokens)
}

func TestCompatRedactedThinkingStreamRoundTrip(t *testing.T) {
	values, _ := compatStream(t, types.RelayFormatClaude, types.RelayFormatOpenAIResponses, []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"signed-redacted"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
	})
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatClaude, ResponseStreamOptions{})
	require.NoError(t, err)
	var blocks []dto.ClaudeMediaMessage
	for _, value := range values {
		event := compatResponsesEvent(t, value)
		results, err := ConvertStreamResponseChunk(context.Background(), nil, state, &event)
		require.NoError(t, err)
		for _, result := range results {
			native := result.Value.(*dto.ClaudeResponse)
			if native.ContentBlock != nil {
				blocks = append(blocks, *native.ContentBlock)
			}
		}
	}
	require.Len(t, blocks, 1)
	require.Equal(t, "redacted_thinking", blocks[0].Type)
	require.Equal(t, "signed-redacted", blocks[0].Data)
}

func TestCompatToolSearchResponseLifecycle(t *testing.T) {
	info := &convmeta.Values{ResponsesTools: &convmeta.ResponsesToolState{ToolSearch: true}}
	source := compatDecode[dto.OpenAITextResponse](t, `{"id":"chat_search","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"search_1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"files\"}"}}]},"finish_reason":"tool_calls"}]}`)
	result, err := ConvertResponse(context.Background(), info, types.RelayFormatOpenAIResponses, source)
	require.NoError(t, err)
	response := result.Value.(*dto.OpenAIResponsesResponse)
	require.Len(t, response.Output, 1)
	wire, err := kitutil.Marshal(response.Output[0])
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"tool_search_call","id":"search_1","call_id":"search_1","status":"completed","execution":"client","arguments":{"query":"files"}}`, string(wire))
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{EmitSequenceNumber: true})
	require.NoError(t, err)
	sourceChunk := compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"search_1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"files\"}"}}]},"finish_reason":"tool_calls"}]}`)
	results, err := ConvertStreamResponseChunk(context.Background(), info, state, sourceChunk)
	require.NoError(t, err)
	final, err := FinalizeStreamResponse(context.Background(), info, state)
	require.NoError(t, err)
	results = append(results, final...)
	var done int
	for _, result := range results {
		event := compatResponsesEvent(t, result.Value)
		require.NotEqual(t, "response.function_call_arguments.delta", event.Type)
		require.NotEqual(t, "response.function_call_arguments.done", event.Type)
		if event.Type == "response.output_item.done" {
			done++
			require.Equal(t, "tool_search_call", event.Item.Type)
			require.JSONEq(t, `{"query":"files"}`, string(event.Item.Arguments))
		}
	}
	require.Equal(t, 1, done)
}

func TestCompatChatResponseDecodesUsageWithoutHidingChoices(t *testing.T) {
	source := compatDecode[dto.OpenAITextResponse](t, `{"id":"chat_1","model":"test-model","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"test","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":30,"cache_creation_tokens":10}}}`)
	require.Equal(t, "test-model", source.Model)
	require.Len(t, source.Choices, 1)
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatClaude, source)
	require.NoError(t, err)
	response := result.Value.(*dto.ClaudeResponse)
	require.Len(t, response.Content, 1)
	require.Equal(t, "test", response.Content[0].Name)
	require.Equal(t, 60, response.Usage.InputTokens)
	require.Equal(t, 10, response.Usage.CacheCreationInputTokens)
}

func TestCompatClaudeCodeReadEmptyPagesIsOptIn(t *testing.T) {
	source := compatDecode[dto.OpenAIResponsesResponse](t, `{"id":"resp_read","output":[{"type":"function_call","name":"Read","call_id":"call_read","arguments":"{\"file_path\":\"/tmp/a\",\"pages\":\"\"}"}]}`)
	for _, enabled := range []bool{false, true} {
		options := &convmeta.Options{}
		options.Claude.ClaudeCodeReadEmptyPages = enabled
		result, err := ConvertResponse(context.Background(), &convmeta.Values{Options: options}, types.RelayFormatClaude, source)
		require.NoError(t, err)
		block := result.Value.(*dto.ClaudeResponse).Content[0]
		wire, err := kitutil.Marshal(block.Input)
		require.NoError(t, err)
		if enabled {
			require.JSONEq(t, `{"file_path":"/tmp/a"}`, string(wire))
		} else {
			require.JSONEq(t, `{"file_path":"/tmp/a","pages":""}`, string(wire))
		}
	}
}

func TestCompatReasoningOnlyVisibleFallbackPolicy(t *testing.T) {
	for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		for _, enabled := range []bool{false, true} {
			info := &convmeta.Values{Options: &convmeta.Options{ReasoningOnlyAsText: enabled}}
			source := compatDecode[dto.OpenAITextResponse](t, `{"id":"chat_reason","model":"deepseek","choices":[{"message":{"role":"assistant","reasoning_content":"answer"},"finish_reason":"stop"}]}`)
			result, err := ConvertResponse(context.Background(), info, target, source)
			require.NoError(t, err)
			switch response := result.Value.(type) {
			case *dto.ClaudeResponse:
				count := 0
				for _, block := range response.Content {
					if block.Type == "text" && block.GetText() == "answer" {
						count++
					}
				}
				require.Equal(t, enabled, count == 1)
			case *dto.OpenAIResponsesResponse:
				count := 0
				for _, item := range response.Output {
					if item.Type == "message" && len(item.Content) > 0 && item.Content[0].Text == "answer" {
						count++
					}
				}
				require.Equal(t, enabled, count == 1)
			}
			info = &convmeta.Values{Options: &convmeta.Options{ReasoningOnlyAsText: enabled}}
			state, err := NewResponseStreamState(types.RelayFormatOpenAI, target, ResponseStreamOptions{})
			require.NoError(t, err)
			chunk := compatDecode[dto.ChatCompletionsStreamResponse](t, `{"id":"chat_reason","choices":[{"delta":{"reasoning_content":"answer"},"finish_reason":"stop"}]}`)
			output, err := ConvertStreamResponseChunk(context.Background(), info, state, chunk)
			require.NoError(t, err)
			final, err := FinalizeStreamResponse(context.Background(), info, state)
			require.NoError(t, err)
			output = append(output, final...)
			var text string
			for _, event := range output {
				switch value := event.Value.(type) {
				case *dto.ClaudeResponse:
					if value.Delta != nil && value.Delta.Type == "text_delta" {
						text += value.Delta.GetText()
					}
				default:
					wire := compatResponsesEvent(t, event.Value)
					if wire.Type == "response.output_text.delta" {
						text += wire.Delta
					}
				}
			}
			if enabled {
				require.Equal(t, "answer", text)
			} else {
				require.Empty(t, text)
			}
		}
	}
}

func TestCompatReadStreamSanitizesAsSoonAsJSONCompletes(t *testing.T) {
	options := &convmeta.Options{}
	options.Claude.ClaudeCodeReadEmptyPages = true
	info := &convmeta.Values{Options: options}
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatClaude, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), info, state, compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"Read","call_id":"call_read"}}`))
	require.NoError(t, err)
	first, err := ConvertStreamResponseChunk(context.Background(), info, state, compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"file_path\":\"/tmp/a\","}`))
	require.NoError(t, err)
	require.Empty(t, first)
	second, err := ConvertStreamResponseChunk(context.Background(), info, state, compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"pages\":\"\"}"}`))
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.JSONEq(t, `{"file_path":"/tmp/a"}`, *second[0].Value.(*dto.ClaudeResponse).Delta.PartialJson)
}

func TestCompatResponsesRefusalSurvivesChatAndBufferedStream(t *testing.T) {
	source := compatDecode[dto.OpenAIResponsesResponse](t, `{"id":"r","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"cannot help"}]}]}`)
	converted, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAI, source)
	require.NoError(t, err)
	require.Equal(t, "cannot help", *converted.Value.(*dto.OpenAITextResponse).Choices[0].Message.Refusal)
	accumulator := NewResponsesBufferedAccumulator()
	accumulator.ProcessEvent(compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.refusal.delta","output_index":0,"delta":"cannot "}`))
	accumulator.ProcessEvent(compatDecode[dto.ResponsesStreamResponse](t, `{"type":"response.refusal.delta","output_index":0,"delta":"help"}`))
	final := &dto.OpenAIResponsesResponse{ID: "r"}
	accumulator.SupplementResponseOutput(final)
	require.Len(t, final.Output, 1)
	require.Equal(t, "cannot help", final.Output[0].Content[0].Refusal)
}

func TestCompatClaudeStreamEmptyArgumentsAndMissingName(t *testing.T) {
	for _, name := range []string{"noop", ""} {
		info := &convmeta.Values{}
		state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatClaude, ResponseStreamOptions{})
		require.NoError(t, err)
		chunk := compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_empty","function":{"arguments":""}}]},"finish_reason":"tool_calls"}]}`)
		chunk.Choices[0].Delta.ToolCalls[0].Function.Name = name
		results, err := ConvertStreamResponseChunk(context.Background(), info, state, chunk)
		if name == "" {
			require.ErrorContains(t, err, "missing a function name")
			continue
		}
		require.NoError(t, err)
		final, err := FinalizeStreamResponse(context.Background(), info, state)
		require.NoError(t, err)
		var args strings.Builder
		for _, result := range append(results, final...) {
			event := result.Value.(*dto.ClaudeResponse)
			if event.Delta != nil && event.Delta.PartialJson != nil {
				args.WriteString(*event.Delta.PartialJson)
			}
		}
		require.JSONEq(t, `{}`, args.String())
	}
}

func TestCompatResponsesToClaudeSequentialBlocksAndTerminalRecovery(t *testing.T) {
	values, _ := compatStream(t, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"first"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"noop"}}`,
		`{"type":"response.function_call_arguments.done","output_index":1,"arguments":"{}"}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"do not replay unrelated terminal parts"}]}]}}`,
	})
	var text strings.Builder
	open := false
	for _, value := range values {
		event := value.(*dto.ClaudeResponse)
		switch event.Type {
		case "content_block_start":
			require.False(t, open)
			open = true
		case "content_block_delta":
			require.True(t, open)
			if event.Delta.Type == "text_delta" {
				text.WriteString(event.Delta.GetText())
			}
		case "content_block_stop":
			require.True(t, open)
			open = false
		}
	}
	require.False(t, open)
	require.Equal(t, "first", text.String())
}

func TestCompatReasoningOnlyWhitespaceDoesNotBecomeText(t *testing.T) {
	for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		info := &convmeta.Values{Options: &convmeta.Options{ReasoningOnlyAsText: true}}
		state, err := NewResponseStreamState(types.RelayFormatOpenAI, target, ResponseStreamOptions{})
		require.NoError(t, err)
		results, err := ConvertStreamResponseChunk(context.Background(), info, state, compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"delta":{"reasoning_content":"  \n"},"finish_reason":"stop"}]}`))
		require.NoError(t, err)
		final, err := FinalizeStreamResponse(context.Background(), info, state)
		require.NoError(t, err)
		for _, result := range append(results, final...) {
			if event, ok := result.Value.(*dto.ClaudeResponse); ok {
				if event.Delta != nil {
					require.NotEqual(t, "text_delta", event.Delta.Type)
				}
			} else {
				require.NotEqual(t, "response.output_text.delta", compatResponsesEvent(t, result.Value).Type)
			}
		}
	}
}

func TestCompatTruncatedToolArgumentsNeverEmitDone(t *testing.T) {
	for _, reason := range []string{"length", "tool_calls", "transport_done"} {
		state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{RequireTerminalEvent: true})
		require.NoError(t, err)
		_, err = ConvertStreamResponseChunk(context.Background(), nil, state, compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_partial","function":{"name":"run","arguments":"{\"cmd\":\"unfinished"}}]}}]}`))
		require.NoError(t, err)
		var output []ResponseResult
		if reason == "transport_done" {
			state.MarkTransportComplete()
			output, err = FinalizeStreamResponse(context.Background(), nil, state)
		} else {
			terminal := compatDecode[dto.ChatCompletionsStreamResponse](t, `{"choices":[{"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}`)
			terminal.Choices[0].FinishReason = &reason
			output, err = ConvertStreamResponseChunk(context.Background(), nil, state, terminal)
			require.Equal(t, 6, state.Usage().CompletionTokens)
		}
		require.ErrorContains(t, err, "invalid JSON")
		require.Empty(t, output)
		failed, handled := state.FailResponsesStream("invalid_tool_arguments", "invalid JSON", "")
		require.True(t, handled)
		for _, result := range failed {
			event := compatResponsesEvent(t, result.Value)
			require.NotEqual(t, "response.function_call_arguments.done", event.Type)
			require.NotEqual(t, "response.output_item.done", event.Type)
		}
	}
}
