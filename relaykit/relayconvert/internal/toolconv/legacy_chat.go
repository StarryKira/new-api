package toolconv

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// Legacy Chat histories identify function results by name, not call ID. Lower
// them once before routing so every target sees the same paired tool history.
func normalizeLegacyChatMessages(messages []dto.Message) ([]dto.Message, error) {
	out := append([]dto.Message(nil), messages...)
	used := make(map[string]bool)
	for _, message := range messages {
		used[message.ToolCallId] = true
		for _, call := range message.ParseToolCalls() {
			used[call.ID] = true
		}
	}
	pending := make(map[string][]string)
	next := 0
	for i := range out {
		message := &out[i]
		if message.Role == "assistant" && len(message.FunctionCall) > 0 && len(message.ParseToolCalls()) == 0 {
			var function dto.FunctionRequest
			if err := kitutil.Unmarshal(message.FunctionCall, &function); err != nil {
				return nil, fmt.Errorf("messages[%d].function_call: %w", i, err)
			}
			if strings.TrimSpace(function.Name) == "" {
				return nil, fmt.Errorf("messages[%d].function_call requires name", i)
			}
			var id string
			for {
				next++
				id = fmt.Sprintf("call_legacy_%d", next)
				if !used[id] {
					break
				}
			}
			used[id] = true
			pending[function.Name] = append(pending[function.Name], id)
			message.ToolCalls, _ = kitutil.Marshal([]dto.ToolCallRequest{{ID: id, Type: "function", Function: function}})
		}
		message.FunctionCall = nil
		if message.Role != "function" {
			continue
		}
		if message.ToolCallId == "" && message.Name != nil {
			queue := pending[*message.Name]
			if len(queue) > 0 {
				message.ToolCallId = queue[0]
				pending[*message.Name] = queue[1:]
			}
		}
		if message.ToolCallId != "" {
			message.Role = "tool"
		}
	}
	return out, nil
}
