package dto

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// NormalizeResponsesUsageJSON canonicalizes provider usage aliases without
// installing an unmarshaler on Usage, which is anonymously embedded by Chat
// response DTOs. Explicit nested cache-write counts, including zero, take
// precedence over legacy top-level aliases.
func NormalizeResponsesUsageJSON(data []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := kitutil.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return data, nil
	}
	number := func(name string) int { var value int; _ = kitutil.Unmarshal(fields[name], &value); return value }
	set := func(name string, value any) { raw, _ := kitutil.Marshal(value); fields[name] = raw }
	input, output := number("input_tokens"), number("output_tokens")
	if input == 0 {
		input = number("prompt_tokens")
	}
	if output == 0 {
		output = number("completion_tokens")
	}
	set("input_tokens", input)
	set("output_tokens", output)
	if number("total_tokens") == 0 {
		set("total_tokens", input+output)
	}
	nested := func(name string) map[string]json.RawMessage {
		var result map[string]json.RawMessage
		_ = kitutil.Unmarshal(fields[name], &result)
		meaningful := false
		for key, raw := range result {
			if key == "cache_write_tokens" || key == "cache_creation_tokens" || key == "cached_creation_tokens" || string(raw) != "0" && string(raw) != "null" && string(raw) != `""` {
				meaningful = true
				break
			}
		}
		if !meaningful {
			return nil
		}
		return result
	}
	inDetails, promptDetails := nested("input_tokens_details"), nested("prompt_tokens_details")
	canonical := inDetails
	if canonical == nil {
		canonical = promptDetails
	}
	if _, ok := fields["output_tokens_details"]; !ok && nested("completion_tokens_details") != nil {
		fields["output_tokens_details"] = fields["completion_tokens_details"]
	}
	count := number("cache_creation_input_tokens")
	if count == 0 {
		for _, name := range []string{"cache_write_input_tokens", "cache_creation_tokens", "cache_write_tokens"} {
			if number(name) > 0 {
				count = number(name)
				break
			}
		}
	}
	var authoritative bool
	for _, name := range []string{"cache_write_tokens", "cached_creation_tokens", "cache_creation_tokens"} {
		for _, details := range []map[string]json.RawMessage{inDetails, promptDetails} {
			if raw, ok := details[name]; ok {
				if err := kitutil.Unmarshal(raw, &count); err != nil {
					return nil, err
				}
				count = max(count, 0)
				authoritative = true
				break
			}
		}
		if authoritative {
			break
		}
	}
	if canonical != nil || count > 0 || authoritative {
		if canonical == nil {
			canonical = map[string]json.RawMessage{}
		}
		delete(canonical, "cache_creation_tokens")
		// A canonical write count and legacy creation count describe the same
		// tokens. Normalize both so later aggregation cannot reintroduce stale data.
		if count > 0 || authoritative {
			raw, _ := kitutil.Marshal(count)
			canonical["cached_creation_tokens"] = raw
			if _, ok := canonical["cache_write_tokens"]; ok {
				canonical["cache_write_tokens"] = raw
			}
		}
		set("input_tokens_details", canonical)
	}
	if count > 0 || authoritative {
		set("cache_creation_input_tokens", count)
	}
	return kitutil.Marshal(fields)
}

func (r *OpenAIResponsesResponse) UnmarshalJSON(data []byte) error {
	type plain OpenAIResponsesResponse
	var decoded struct {
		plain
		Usage json.RawMessage `json:"usage"`
	}
	if err := kitutil.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = OpenAIResponsesResponse(decoded.plain)
	if len(decoded.Usage) > 0 && string(decoded.Usage) != "null" {
		normalized, err := NormalizeResponsesUsageJSON(decoded.Usage)
		if err != nil {
			return err
		}
		return kitutil.Unmarshal(normalized, &r.Usage)
	}
	return nil
}

func (r *ResponsesStreamResponse) UnmarshalJSON(data []byte) error {
	type plain ResponsesStreamResponse
	var decoded struct {
		plain
		Usage json.RawMessage `json:"usage"`
	}
	if err := kitutil.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = ResponsesStreamResponse(decoded.plain)
	if len(decoded.Usage) > 0 && string(decoded.Usage) != "null" {
		normalized, err := NormalizeResponsesUsageJSON(decoded.Usage)
		if err != nil {
			return err
		}
		return kitutil.Unmarshal(normalized, &r.Usage)
	}
	return nil
}
