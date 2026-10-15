package oairesponses

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// Interrupted clients can send incomplete tool batches or place a notice between
// calls and results. Repair only when requested by the gateway's history policy.
func repairChatToolHistory(messages []dto.Message) []dto.Message {
	replies := map[string]dto.Message{}
	for _, message := range messages {
		if message.Role == "tool" && message.ToolCallId != "" {
			replies[message.ToolCallId] = message
		}
	}
	out := make([]dto.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "tool" && message.ToolCallId != "" {
			continue
		}
		calls := message.ParseToolCalls()
		if len(calls) == 0 {
			out = append(out, message)
			continue
		}
		kept := make([]dto.ToolCallRequest, 0, len(calls))
		for _, call := range calls {
			if _, ok := replies[call.ID]; ok && call.ID != "" {
				kept = append(kept, call)
			}
		}
		if len(kept) == 0 {
			message.ToolCalls = nil
			if strings.TrimSpace(message.StringContent()) != "" {
				out = append(out, message)
			}
			continue
		}
		message.ToolCalls, _ = kitutil.Marshal(kept)
		out = append(out, message)
		for _, call := range kept {
			out = append(out, replies[call.ID])
		}
	}
	return out
}

func repairClaudeToolHistory(messages []dto.ClaudeMessage) []dto.ClaudeMessage {
	results := map[string]dto.ClaudeMediaMessage{}
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		for _, block := range claudeMessageContentParts(message.Content) {
			if block.Type == "tool_result" && block.ToolUseId != "" {
				results[block.ToolUseId] = block
			}
		}
	}
	out := make([]dto.ClaudeMessage, 0, len(messages))
	for _, message := range messages {
		blocks := claudeMessageContentParts(message.Content)
		kept := make([]dto.ClaudeMediaMessage, 0, len(blocks))
		replies := make([]dto.ClaudeMediaMessage, 0)
		for _, block := range blocks {
			if message.Role == "user" && block.Type == "tool_result" {
				continue
			}
			if message.Role == "assistant" && block.Type == "tool_use" {
				result, ok := results[block.Id]
				if !ok {
					continue
				}
				replies = append(replies, result)
			}
			if block.Type == "text" && strings.TrimSpace(block.GetText()) == "" {
				continue
			}
			kept = append(kept, block)
		}
		if len(kept) > 0 {
			message.Content = kept
			out = append(out, message)
		}
		if len(replies) > 0 {
			out = append(out, dto.ClaudeMessage{Role: "user", Content: replies})
		}
	}
	// Anthropic requires alternating turns; concatenation retains signed block order.
	merged := make([]dto.ClaudeMessage, 0, len(out))
	for _, message := range out {
		if len(merged) > 0 && merged[len(merged)-1].Role == message.Role {
			previous := &merged[len(merged)-1]
			previous.Content = append(claudeMessageContentParts(previous.Content), claudeMessageContentParts(message.Content)...)
		} else {
			merged = append(merged, message)
		}
	}
	return merged
}

// Repair item history before extracting media so discarded or superseded tool
// outputs cannot leave orphan images in later user messages.
func repairResponsesToolItems(items []map[string]any) []map[string]any {
	outputs := map[string]map[string]any{}
	outputIndex := map[string]int{}
	lastCallIndex := map[string]int{}
	for index, item := range items {
		typ, _ := item["type"].(string)
		id, _ := item["call_id"].(string)
		if id == "" {
			continue
		}
		if typ == responsesInputTypeFunctionCallOutput || typ == responsesInputTypeCustomToolOutput {
			outputs[id] = item
			outputIndex[id] = index
		} else if typ == responsesInputTypeFunctionCall || typ == responsesInputTypeCustomToolCall {
			lastCallIndex[id] = index
		}
	}
	out := make([]map[string]any, 0, len(items))
	var pending []map[string]any
	flush := func() { out = append(out, pending...); pending = nil }
	for index, item := range items {
		typ, _ := item["type"].(string)
		if typ == responsesInputTypeFunctionCallOutput || typ == responsesInputTypeCustomToolOutput {
			flush()
			continue
		}
		if typ != responsesInputTypeFunctionCall && typ != responsesInputTypeCustomToolCall {
			flush()
			out = append(out, item)
			continue
		}
		id, _ := item["call_id"].(string)
		result, found := outputs[id]
		if !found || id == "" || lastCallIndex[id] != index || outputIndex[id] < index || responsesCallName(item) == "" {
			continue
		}
		wantOutput := responsesInputTypeFunctionCallOutput
		if typ == responsesInputTypeCustomToolCall {
			wantOutput = responsesInputTypeCustomToolOutput
		}
		if result["type"] != wantOutput {
			continue
		}
		if typ == responsesInputTypeFunctionCall {
			if args, ok := item["arguments"].(string); ok && strings.TrimSpace(args) != "" {
				var object map[string]any
				if kitutil.Unmarshal([]byte(args), &object) != nil || object == nil {
					continue
				}
			}
		}
		out = append(out, item)
		pending = append(pending, result)
	}
	flush()
	return out
}
