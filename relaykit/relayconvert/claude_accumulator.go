package relayconvert

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ClaudeBufferedAccumulator reconstructs a native Messages response from its
// stream while preserving unknown message and content-block fields. Real input
// deltas replace the start-event seed; the seed survives when no deltas arrive.
type ClaudeBufferedAccumulator struct {
	message    map[string]json.RawMessage
	blocks     map[int]map[string]json.RawMessage
	arguments  map[int]string
	err        error
	terminated bool
}

func NewClaudeBufferedAccumulator() *ClaudeBufferedAccumulator {
	return &ClaudeBufferedAccumulator{blocks: make(map[int]map[string]json.RawMessage), arguments: make(map[int]string)}
}

func (a *ClaudeBufferedAccumulator) ProcessEventJSON(data []byte) error {
	if a.err != nil {
		return a.err
	}
	a.err = a.process(data)
	return a.err
}

func (a *ClaudeBufferedAccumulator) process(data []byte) error {
	var event struct {
		Type    string                     `json:"type"`
		Index   int                        `json:"index"`
		Message map[string]json.RawMessage `json:"message"`
		Block   map[string]json.RawMessage `json:"content_block"`
		Delta   map[string]json.RawMessage `json:"delta"`
		Usage   map[string]json.RawMessage `json:"usage"`
		Error   json.RawMessage            `json:"error"`
	}
	if err := kitutil.Unmarshal(data, &event); err != nil {
		return err
	}
	if event.Type == "error" {
		return fmt.Errorf("upstream Claude stream error: %s", event.Error)
	}
	switch event.Type {
	case "message_start":
		if a.message != nil {
			return fmt.Errorf("duplicate Claude message_start")
		}
		a.message = event.Message
		if a.message == nil {
			return fmt.Errorf("Claude message_start has no message")
		}
	case "content_block_start":
		if event.Index < 0 {
			return nil
		}
		if event.Block == nil {
			return fmt.Errorf("invalid Claude content block")
		}
		a.blocks[event.Index] = event.Block
	case "content_block_delta":
		if event.Index < 0 {
			return nil
		}
		block := a.blocks[event.Index]
		if block == nil {
			return fmt.Errorf("Claude delta for unknown content index %d", event.Index)
		}
		var kind string
		if err := kitutil.Unmarshal(event.Delta["type"], &kind); err != nil {
			return err
		}
		switch kind {
		case "text_delta", "thinking_delta", "signature_delta":
			key := map[string]string{"text_delta": "text", "thinking_delta": "thinking", "signature_delta": "signature"}[kind]
			var prior, delta string
			if raw := block[key]; len(raw) > 0 {
				if err := kitutil.Unmarshal(raw, &prior); err != nil {
					return err
				}
			}
			if err := kitutil.Unmarshal(event.Delta[key], &delta); err != nil {
				return err
			}
			block[key], _ = kitutil.Marshal(prior + delta)
		case "input_json_delta":
			var delta string
			if err := kitutil.Unmarshal(event.Delta["partial_json"], &delta); err != nil {
				return err
			}
			a.arguments[event.Index] += delta
		case "citations_delta":
			var citations []json.RawMessage
			if raw := block["citations"]; len(raw) > 0 {
				if err := kitutil.Unmarshal(raw, &citations); err != nil {
					return err
				}
			}
			if raw := event.Delta["citation"]; len(raw) > 0 {
				citations = append(citations, raw)
			}
			block["citations"], _ = kitutil.Marshal(citations)
		}
	case "message_stop":
		a.terminated = true
	case "message_delta":
		if a.message == nil {
			return fmt.Errorf("Claude message_delta before message_start")
		}
		var stopReason string
		if raw := event.Delta["stop_reason"]; len(raw) > 0 {
			if err := kitutil.Unmarshal(raw, &stopReason); err != nil {
				return err
			}
			a.terminated = a.terminated || strings.TrimSpace(stopReason) != ""
		}
		for key, value := range event.Delta {
			if key != "type" {
				a.message[key] = value
			}
		}
		var usage map[string]json.RawMessage
		if raw := a.message["usage"]; len(raw) > 0 {
			if err := kitutil.Unmarshal(raw, &usage); err != nil {
				return err
			}
		}
		if usage == nil {
			usage = make(map[string]json.RawMessage)
		}
		for key, value := range event.Usage {
			usage[key] = value
		}
		a.message["usage"], _ = kitutil.Marshal(usage)
	}
	return nil
}

func (a *ClaudeBufferedAccumulator) ResponseJSON() ([]byte, error) {
	if a.err != nil {
		return nil, a.err
	}
	if a.message == nil {
		return nil, fmt.Errorf("Claude stream ended without message_start")
	}
	if !a.terminated {
		return nil, fmt.Errorf("%w: Claude stream", ErrIncompleteResponseStream)
	}
	indices := make([]int, 0, len(a.blocks))
	for index := range a.blocks {
		indices = append(indices, index)
	}
	slices.Sort(indices)
	if len(indices) > 0 {
		blocks := make([]map[string]json.RawMessage, 0, len(indices))
		for _, index := range indices {
			block := a.blocks[index]
			if args, ok := a.arguments[index]; ok {
				if !kitutil.Valid([]byte(args)) {
					return nil, fmt.Errorf("invalid Claude tool arguments at content index %d", index)
				}
				block["input"] = json.RawMessage(args)
			}
			blocks = append(blocks, block)
		}
		a.message["content"], _ = kitutil.Marshal(blocks)
	}
	return kitutil.Marshal(a.message)
}
