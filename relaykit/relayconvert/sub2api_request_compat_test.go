package relayconvert

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// sub2api e47255715: accounting must use the effort rendered for the target.
// Kimi Code accepts named effort on its Anthropic surface, not a manual budget.
func TestKimiCodeAnthropicReasoningUsesNamedEffort(t *testing.T) {
	for _, model := range []string{"k3", "k3-256k"} {
		for _, test := range []struct {
			effort string
			want   string
		}{
			{"", ""}, {"none", ""}, {"minimal", "low"}, {"low", "low"},
			{"medium", "high"}, {"high", "high"}, {"xhigh", "max"}, {"max", "max"},
		} {
			t.Run(model+"/"+test.effort, func(t *testing.T) {
				var req dto.OpenAIResponsesRequest
				require.NoError(t, kitutil.Unmarshal([]byte(fmt.Sprintf(`{"model":%q,"input":"hello","reasoning":{"effort":%q}}`, model, test.effort)), &req))
				meta := &convmeta.Values{OriginModelName: "claude-fable-5-1", UpstreamModelName: model, Options: &convmeta.Options{Claude: convmeta.ClaudeOptions{DefaultMaxTokens: func(string) int { return 8192 }}}}
				result, err := ConvertRequest(context.Background(), meta, types.RelayFormatClaude, &req)
				require.NoError(t, err)
				out := result.Value.(*dto.ClaudeRequest)
				require.Equal(t, test.want, out.GetEfforts())
				wantRecord := test.want
				if test.effort == "none" {
					wantRecord = "none"
				}
				require.Equal(t, wantRecord, meta.GetReasoningEffort())
				if test.effort == "" {
					require.Nil(t, out.Thinking)
				} else {
					require.NotNil(t, out.Thinking)
					require.Nil(t, out.Thinking.BudgetTokens)
					if test.effort == "none" {
						require.Equal(t, "disabled", out.Thinking.Type)
					} else {
						require.Equal(t, "enabled", out.Thinking.Type)
					}
				}
			})
		}
	}
}

// sub2api 9a8ab4f88 / 7a767d5ce: developer priority and legacy history must
// survive conversion; legacy call IDs are deterministic and cannot collide.
func TestChatRequestCompatibilityDeveloperAndLegacyCalls(t *testing.T) {
	var req dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","messages":[{"role":"developer","content":"rules"},{"role":"assistant","function_call":{"name":"lookup","arguments":"{}"}},{"role":"function","name":"lookup","content":"first"},{"role":"assistant","function_call":{"name":"lookup","arguments":"{}"}},{"role":"function","name":"lookup","content":"second"},{"role":"assistant","tool_calls":[{"id":"call_legacy_1","type":"function","function":{"name":"modern","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_legacy_1","content":"third"}]}`), &req))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, &req)
	require.NoError(t, err)
	out := result.Value.(*dto.OpenAIResponsesRequest)
	var items []map[string]any
	require.NoError(t, kitutil.Unmarshal(out.Input, &items))
	require.Equal(t, "developer", items[0]["role"])
	require.Equal(t, "message", items[0]["type"])
	calls := []string{}
	outputs := []string{}
	for _, item := range items {
		if item["type"] == "function_call" {
			calls = append(calls, item["call_id"].(string))
		}
		if item["type"] == "function_call_output" {
			outputs = append(outputs, item["call_id"].(string))
		}
	}
	require.Equal(t, []string{"call_legacy_2", "call_legacy_3", "call_legacy_1"}, calls)
	require.Equal(t, calls, outputs)
	require.NotEmpty(t, req.Messages[1].FunctionCall, "conversion must not mutate caller history")
}

// sub2api df8eceba0 / 401dd43b4: Codex carries task bodies in agent_message;
// thinking providers require the same reasoning on chained assistant calls.
func TestResponsesRequestCompatibilityAgentMessageAndReasoning(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`[
 {"type":"agent_message","content":[{"type":"input_text","text":"Payload: "},{"type":"encrypted_content","encrypted_content":"do the work"}]},
 {"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]},
 {"type":"function_call","call_id":"a","name":"one","arguments":"{}"},
 {"type":"function_call_output","call_id":"a","output":"ok"},
 {"type":"function_call","call_id":"b","name":"two","arguments":"{}"},
 {"type":"function_call_output","call_id":"b","output":"ok"},
 {"type":"message","role":"user","content":"next"},
 {"type":"message","role":"assistant","content":"answer"}
 ]`)}
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, req)
	require.NoError(t, err)
	out := result.Value.(*dto.GeneralOpenAIRequest)
	require.Len(t, out.Messages, 7)
	require.Equal(t, "Payload: do the work", out.Messages[0].Content)
	require.Equal(t, "plan", out.Messages[1].GetReasoningContent())
	require.Equal(t, "plan", out.Messages[3].GetReasoningContent())
	require.Empty(t, out.Messages[6].GetReasoningContent())
}

func TestResponsesRequestCompatibilityNamedChoiceAndDisabledThinking(t *testing.T) {
	var chat dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`), &chat))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, &chat)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"function","name":"lookup"}`, string(result.Value.(*dto.OpenAIResponsesRequest).ToolChoice))
	var claude dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"},"output_config":{"effort":"high"}}`), &claude))
	result, err = ConvertRequest(context.Background(), &convmeta.Values{}, types.RelayFormatOpenAIResponses, &claude)
	require.NoError(t, err)
	require.Equal(t, "none", result.Value.(*dto.OpenAIResponsesRequest).Reasoning.Effort)
}

// sub2api 51631d032/9ede0f716: a completed tool-search discovery becomes
// callable on the next turn, including custom tools in namespaces.
func TestResponsesRequestCompatibilityToolSearchDiscovery(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Tools: json.RawMessage(`[{"type":"tool_search"}]`), ToolChoice: json.RawMessage(`{"type":"tool_search"}`), Input: json.RawMessage(`[
 {"type":"message","role":"user","content":"search"},
 {"type":"tool_search_call","call_id":"s","arguments":{"query":"files"}},
 {"type":"tool_search_output","call_id":"s","status":"completed","tools":[{"type":"namespace","name":"files","tools":[{"type":"function","name":"read","parameters":{"type":"object"}},{"type":"custom","name":"write"}]}]},
 {"type":"function_call","call_id":"r","namespace":"files","name":"read","arguments":"{}"},
 {"type":"function_call_output","call_id":"r","output":"ok"}
 ]`)}
	meta := &convmeta.Values{}
	result, err := ConvertRequest(context.Background(), meta, types.RelayFormatOpenAI, req)
	require.NoError(t, err)
	out := result.Value.(*dto.GeneralOpenAIRequest)
	require.Len(t, out.Tools, 3)
	require.Equal(t, "tool_search", out.Tools[0].Function.Name)
	require.Equal(t, "files__read", out.Tools[1].Function.Name)
	require.Equal(t, "files__write", out.Tools[2].Function.Name)
	choice, _ := kitutil.Marshal(out.ToolChoice)
	require.JSONEq(t, `{"type":"function","function":{"name":"tool_search"}}`, string(choice))
	require.True(t, meta.ResponsesToolState().ToolSearch)
	require.Equal(t, "tool_search", out.Messages[1].ParseToolCalls()[0].Function.Name)
	require.Equal(t, "tool", out.Messages[2].Role)
	require.Contains(t, out.Messages[2].StringContent(), "read")
	require.Equal(t, "files__read", out.Messages[3].ParseToolCalls()[0].Function.Name)
}

func TestResponsesRequestCompatibilityToolSearchConflict(t *testing.T) {
	for _, tools := range []string{
		`[{"type":"tool_search"},{"type":"function","name":"tool_search","parameters":{"type":"object"}}]`,
		`[{"type":"tool_search"},{"type":"function","name":"read","parameters":{"type":"object","properties":{"old":{"type":"string"}}}}]`,
	} {
		req := &dto.OpenAIResponsesRequest{Model: "test", Tools: json.RawMessage(tools), Input: json.RawMessage(`[{"type":"tool_search_output","tools":[{"type":"function","name":"read","parameters":{"type":"object","properties":{"new":{"type":"string"}}}}]}]`)}
		_, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, req)
		require.Error(t, err)
	}
}

func TestResponsesRequestCompatibilitySignedThinkingRoundTrip(t *testing.T) {
	var response dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"id":"m","type":"message","model":"claude-haiku-5-5","role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"opaque-signature"},{"type":"tool_use","id":"x","name":"lookup","input":{}}],"stop_reason":"tool_use"}`), &response))
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, &response)
	require.NoError(t, err)
	outputs := result.Value.(*dto.OpenAIResponsesResponse).Output
	raw, err := kitutil.Marshal(outputs)
	require.NoError(t, err)
	var input []map[string]any
	require.NoError(t, kitutil.Unmarshal(raw, &input))
	input = append([]map[string]any{{"type": "message", "role": "user", "content": "hi"}}, input...)
	input = append(input, map[string]any{"type": "function_call_output", "call_id": "x", "output": "ok"})
	raw, err = kitutil.Marshal(input)
	require.NoError(t, err)
	require.Contains(t, string(raw), "anthropic-thinking-v1:")
	max := uint(4096)
	replay := &dto.OpenAIResponsesRequest{Model: "claude-haiku-5-5", MaxOutputTokens: &max, Input: raw}
	back, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, replay)
	require.NoError(t, err)
	claude := back.Value.(*dto.ClaudeRequest)
	require.Len(t, claude.Messages, 3)
	blocks, err := claude.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Equal(t, "thinking", blocks[0].Type)
	require.Equal(t, "opaque-signature", blocks[0].Signature)
	require.Equal(t, "plan", *blocks[0].Thinking)
	require.Equal(t, "tool_use", blocks[1].Type)
}

func TestResponsesRequestCompatibilityReasoningCache(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`[{"type":"reasoning","id":"r","encrypted_content":"opaque"},{"type":"function_call","call_id":"a","name":"lookup","arguments":"{}"}]`)}
	meta := &convmeta.Values{Options: &convmeta.Options{ReasoningContentByID: func(id string) string { require.Equal(t, "r", id); return "cached reasoning" }}}
	result, err := ConvertRequest(context.Background(), meta, types.RelayFormatOpenAI, req)
	require.NoError(t, err)
	require.Equal(t, "cached reasoning", result.Value.(*dto.GeneralOpenAIRequest).Messages[0].GetReasoningContent())
}

func TestResponsesRequestCompatibilityNamespaceCollision(t *testing.T) {
	for _, tools := range []string{
		`[{"type":"namespace","name":"files","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]},{"type":"function","name":"files__read","parameters":{"type":"object"}}]`,
		`[{"type":"function","name":"files__read","parameters":{"type":"object"}},{"type":"namespace","name":"files","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}]`,
	} {
		req := &dto.OpenAIResponsesRequest{Model: "test", Tools: json.RawMessage(tools), Input: json.RawMessage(`"hi"`)}
		_, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, req)
		require.ErrorContains(t, err, "ambiguous namespace")
	}
}

// sub2api 26f70c6a8 / c4bdc4847: Haiku 5.5 supports forced tool use,
// unlike Opus/Sonnet 5.5. Invalid requests must not reach upstream unchanged.
func TestResponsesRequestCompatibilityClaude55ForcedTools(t *testing.T) {
	for _, model := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"} {
		t.Run(model, func(t *testing.T) {
			max := uint(4096)
			req := &dto.OpenAIResponsesRequest{Model: model, MaxOutputTokens: &max, Input: json.RawMessage(`"hi"`), Tools: json.RawMessage(`[{"type":"function","name":"lookup","parameters":{"type":"object"}}]`), ToolChoice: json.RawMessage(`{"type":"function","name":"lookup"}`)}
			meta := &convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe}}
			result, err := ConvertRequest(context.Background(), meta, types.RelayFormatClaude, req)
			if model != "claude-haiku-5-5" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			choice, _ := kitutil.Marshal(result.Value.(*dto.ClaudeRequest).ToolChoice)
			require.JSONEq(t, `{"type":"tool","name":"lookup"}`, string(choice))
		})
	}
}

func TestClaudeRequestCompatibilityGrokOpaqueReasoning(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"grok-4.5","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"enc-rs-1"},{"type":"text","text":"done"}]}]}`), &request))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, &request)
	require.NoError(t, err)
	var input []map[string]any
	require.NoError(t, kitutil.Unmarshal(result.Value.(*dto.OpenAIResponsesRequest).Input, &input))
	require.Equal(t, "enc-rs-1", input[0]["encrypted_content"])
}

func TestClaudeRequestCompatibilityCustomNestedTool(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","tools":[{"type":"custom","name":"lookup","custom":{"description":"Find","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}}],"messages":[{"role":"user","content":"hi"}]}`), &request))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, &request)
	require.NoError(t, err)
	tools := result.Value.(*dto.GeneralOpenAIRequest).Tools
	require.Len(t, tools, 1)
	require.Equal(t, "lookup", tools[0].Function.Name)
	require.Equal(t, "Find", tools[0].Function.Description)
	require.NotNil(t, tools[0].Function.Parameters)
}

func TestRequestCompatibilityClaudeURLImageDoesNotFetch(t *testing.T) {
	var chat dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"claude-test","max_tokens":1024,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/pic.png"}}]}]}`), &chat))
	var responses dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"claude-test","max_output_tokens":1024,"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/pic.png"}]}]}`), &responses))
	for _, request := range []any{&chat, &responses} {
		result, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, request)
		require.NoError(t, err)
		blocks, err := result.Value.(*dto.ClaudeRequest).Messages[0].ParseContent()
		require.NoError(t, err)
		require.Equal(t, "url", blocks[0].Source.Type)
		require.Equal(t, "https://example.com/pic.png", blocks[0].Source.Url)
	}
}

func TestRequestCompatibilityHostedChatToolShorthands(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-3","tools":[{"type":"web_search"},{"type":"code_execution"}],"messages":[{"role":"user","content":"hi"}]}`), &request))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &request)
	require.NoError(t, err)
	require.Contains(t, string(result.Value.(*dto.GeminiChatRequest).Tools), "googleSearch")
	require.Contains(t, string(result.Value.(*dto.GeminiChatRequest).Tools), "codeExecution")
}
func TestClaudeRequestCompatibilityRejectMalformedCustomTool(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","tools":[{"type":"custom","name":"invalid"}],"messages":[{"role":"user","content":"hi"}]}`), &request))
	_, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &request)
	require.ErrorContains(t, err, "custom tool requires")
}

func TestRequestCompatibilityMixedSystemContentAndTransportPolicy(t *testing.T) {
	var chat dto.GeneralOpenAIRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","messages":[{"role":"system","content":[{"type":"text","text":"rules"},{"type":"image_url","image_url":{"url":"https://example.com/policy.png"}}]},{"role":"user","content":"hi"}]}`), &chat))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, &chat)
	require.NoError(t, err)
	responses := result.Value.(*dto.OpenAIResponsesRequest)
	require.Empty(t, responses.Instructions)
	require.Contains(t, string(responses.Input), "https://example.com/policy.png")
	require.Contains(t, string(responses.Input), `"role":"system"`)

	claude := &dto.ClaudeRequest{Model: "test", System: "project instructions", Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}}}
	for _, asInput := range []bool{false, true} {
		meta := &convmeta.Values{Options: &convmeta.Options{ResponsesSystemAsInput: asInput}}
		result, err = ConvertRequest(context.Background(), meta, types.RelayFormatOpenAIResponses, claude)
		require.NoError(t, err)
		responses = result.Value.(*dto.OpenAIResponsesRequest)
		if asInput {
			require.Empty(t, responses.Instructions)
			require.Contains(t, string(responses.Input), `"role":"developer"`)
			require.Contains(t, string(responses.Input), "project instructions")
		} else {
			require.JSONEq(t, `"project instructions"`, string(responses.Instructions))
			require.NotContains(t, string(responses.Input), "project instructions")
		}
	}
}

func TestRequestCompatibilityHistoryRepairBeforeMediaExtraction(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`[
 {"type":"function_call","call_id":"a","name":"image","arguments":"{}"},
 {"type":"function_call","call_id":"b","name":"image","arguments":"{}"},
 {"type":"message","role":"developer","content":"retain priority"},
 {"type":"function_call_output","call_id":"a","output":{"content":[{"type":"input_image","image_url":"https://example.com/old.png"}]}},
 {"type":"function_call_output","call_id":"a","output":{"count":9007199254740993,"content":[{"type":"image_url","image_url":{"url":"https://example.com/new.png"}}]}},
 {"type":"function_call_output","call_id":"orphan","output":[{"type":"input_image","image_url":"https://example.com/orphan.png"}]}
 ]`)}
	meta := &convmeta.Values{Options: &convmeta.Options{RepairToolHistory: true}}
	result, err := ConvertRequest(context.Background(), meta, types.RelayFormatOpenAI, req)
	require.NoError(t, err)
	out := result.Value.(*dto.GeneralOpenAIRequest)
	require.Len(t, out.Messages, 4)
	require.Len(t, out.Messages[0].ParseToolCalls(), 1)
	require.Equal(t, "a", out.Messages[0].ParseToolCalls()[0].ID)
	require.Equal(t, "tool", out.Messages[1].Role)
	require.Contains(t, out.Messages[1].StringContent(), "9007199254740993")
	require.Equal(t, "user", out.Messages[2].Role)
	require.Equal(t, "https://example.com/new.png", out.Messages[2].ParseContent()[0].ImageUrl.(*dto.MessageImageUrl).Url)
	require.Equal(t, "developer", out.Messages[3].Role)
	encoded, err := kitutil.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "old.png")
	require.NotContains(t, string(encoded), "orphan.png")
}

func TestRequestCompatibilityHistoryRepairRejectsAmbiguousCalls(t *testing.T) {
	for _, badArguments := range []string{`[]`, `null`, `42`, `"text"`, `{invalid}`} {
		input, err := kitutil.Marshal([]map[string]any{{"type": "function_call", "call_id": "bad", "name": "tool", "arguments": badArguments}, {"type": "function_call_output", "call_id": "bad", "output": "bad result"}, {"type": "message", "role": "user", "content": "continue"}})
		require.NoError(t, err)
		result, err := ConvertRequest(context.Background(), &convmeta.Values{Options: &convmeta.Options{RepairToolHistory: true}}, types.RelayFormatOpenAI, &dto.OpenAIResponsesRequest{Model: "test", Input: input})
		require.NoError(t, err)
		require.Len(t, result.Value.(*dto.GeneralOpenAIRequest).Messages, 1)
	}
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: json.RawMessage(`[
 {"type":"function_call","call_id":"a","name":"old","arguments":"{}"},
 {"type":"function_call_output","call_id":"a","output":"old result"},
 {"type":"function_call","call_id":"a","name":"latest","arguments":"{}"},
 {"type":"function_call_output","call_id":"a","output":"latest result"},
 {"type":"function_call","call_id":"b","name":"wrong_kind","arguments":"{}"},
 {"type":"custom_tool_call_output","call_id":"b","output":"wrong result"}
 ]`)}
	result, err := ConvertRequest(context.Background(), &convmeta.Values{Options: &convmeta.Options{RepairToolHistory: true}}, types.RelayFormatOpenAI, req)
	require.NoError(t, err)
	messages := result.Value.(*dto.GeneralOpenAIRequest).Messages
	require.Len(t, messages, 2)
	require.Equal(t, "latest", messages[0].ParseToolCalls()[0].Function.Name)
	require.Equal(t, "latest result", messages[1].StringContent())
}
