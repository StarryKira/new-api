package relayconvert

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// GeminiBufferedAccumulator preserves native response fields while combining
// streaming candidates. Transport completion is checked by ResponseStreamState;
// this accumulator does not manufacture a finish reason at EOF.
type GeminiBufferedAccumulator struct {
	fields     map[string]json.RawMessage
	candidates map[int]*geminiBufferedCandidate
	order      []int
	err        error
}

type geminiBufferedCandidate struct {
	fields  map[string]json.RawMessage
	content map[string]json.RawMessage
	parts   []json.RawMessage
}

func NewGeminiBufferedAccumulator() *GeminiBufferedAccumulator {
	return &GeminiBufferedAccumulator{fields: make(map[string]json.RawMessage), candidates: make(map[int]*geminiBufferedCandidate)}
}

func (a *GeminiBufferedAccumulator) ProcessEventJSON(data []byte) error {
	if a.err != nil {
		return a.err
	}
	a.err = a.process(data)
	return a.err
}

func (a *GeminiBufferedAccumulator) process(data []byte) error {
	var frame map[string]json.RawMessage
	if err := kitutil.Unmarshal(data, &frame); err != nil {
		return fmt.Errorf("decode Gemini stream frame: %w", err)
	}
	if frame == nil {
		return fmt.Errorf("Gemini stream frame must be an object")
	}
	if failure := frame["error"]; len(failure) > 0 && string(failure) != "null" {
		return fmt.Errorf("Gemini upstream error: %s", failure)
	}
	if len(frame["candidates"]) == 0 && len(frame["usageMetadata"]) == 0 && len(frame["promptFeedback"]) == 0 && len(frame["modelVersion"]) == 0 && len(frame["responseId"]) == 0 {
		return fmt.Errorf("unrecognized Gemini stream frame")
	}
	for key, value := range frame {
		if key != "candidates" {
			a.fields[key] = value
		}
	}
	if len(frame["candidates"]) == 0 {
		return nil
	}
	var candidates []map[string]json.RawMessage
	if err := kitutil.Unmarshal(frame["candidates"], &candidates); err != nil {
		return fmt.Errorf("decode Gemini candidates: %w", err)
	}
	for position, candidate := range candidates {
		if candidate == nil {
			return fmt.Errorf("Gemini candidate must be an object")
		}
		index := position
		if raw := candidate["index"]; len(raw) > 0 {
			if err := kitutil.Unmarshal(raw, &index); err != nil || index < 0 {
				return fmt.Errorf("invalid Gemini candidate index: %s", raw)
			}
		}
		state := a.candidates[index]
		if state == nil {
			state = &geminiBufferedCandidate{fields: make(map[string]json.RawMessage), content: make(map[string]json.RawMessage)}
			a.candidates[index] = state
			a.order = append(a.order, index)
		}
		for key, value := range candidate {
			if key != "content" {
				state.fields[key] = value
			}
		}
		if raw := candidate["content"]; len(raw) > 0 && string(raw) != "null" {
			var content map[string]json.RawMessage
			if err := kitutil.Unmarshal(raw, &content); err != nil {
				return fmt.Errorf("decode Gemini content: %w", err)
			}
			for key, value := range content {
				if key != "parts" {
					state.content[key] = value
				}
			}
			var parts []json.RawMessage
			if raw := content["parts"]; len(raw) > 0 {
				if err := kitutil.Unmarshal(raw, &parts); err != nil {
					return fmt.Errorf("decode Gemini parts: %w", err)
				}
			}
			for _, part := range parts {
				var object map[string]json.RawMessage
				if err := kitutil.Unmarshal(part, &object); err != nil || object == nil {
					return fmt.Errorf("Gemini part must be an object")
				}
				// Only plain adjacent text can be coalesced. Real signatures,
				// thought flags and media/tool boundaries remain attached.
				if len(object) == 1 && len(object["text"]) > 0 && len(state.parts) > 0 {
					var previous map[string]json.RawMessage
					if err := kitutil.Unmarshal(state.parts[len(state.parts)-1], &previous); err == nil && len(previous) == 1 && len(previous["text"]) > 0 {
						var before, after string
						if kitutil.Unmarshal(previous["text"], &before) == nil && kitutil.Unmarshal(object["text"], &after) == nil {
							state.parts[len(state.parts)-1], _ = kitutil.Marshal(map[string]string{"text": before + after})
							continue
						}
					}
				}
				state.parts = append(state.parts, part)
			}
		}
	}
	return nil
}

func (a *GeminiBufferedAccumulator) ResponseJSON() ([]byte, error) {
	if a.err != nil {
		return nil, a.err
	}
	candidates := make([]map[string]json.RawMessage, 0, len(a.order))
	for _, index := range a.order {
		candidate := a.candidates[index]
		if len(candidate.parts) > 0 {
			candidate.content["parts"], _ = kitutil.Marshal(candidate.parts)
		}
		if len(candidate.content) > 0 {
			candidate.fields["content"], _ = kitutil.Marshal(candidate.content)
		}
		candidates = append(candidates, candidate.fields)
	}
	if len(candidates) > 0 {
		a.fields["candidates"], _ = kitutil.Marshal(candidates)
	}
	return kitutil.Marshal(a.fields)
}
