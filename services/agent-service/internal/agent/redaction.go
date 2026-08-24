package agent

import (
	"bytes"
	"encoding/json"
	"strings"
)

const redactedValue = "[REDACTED]"
const maxRedactedStringRunes = 500

var sensitiveJSONKeys = map[string]struct{}{
	"authorization":     {},
	"token":             {},
	"jwt":               {},
	"password":          {},
	"secret":            {},
	"api_key":           {},
	"phone":             {},
	"mobile":            {},
	"address":           {},
	"raw_prompt":        {},
	"provider_response": {},
	"credentials":       {},
}

func RedactJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return json.RawMessage(`{"redaction_error":"invalid_json"}`)
	}
	redacted := redactValue(value, "")
	out, err := json.Marshal(redacted)
	if err != nil {
		return json.RawMessage(`{"redaction_error":"marshal_failed"}`)
	}
	return out
}

func redactValue(value any, key string) any {
	if isSensitiveKey(key) {
		return redactedValue
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = redactValue(childValue, childKey)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, childValue := range typed {
			out[i] = redactValue(childValue, "")
		}
		return out
	case string:
		return truncateRunes(typed, maxRedactedStringRunes)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	_, ok := sensitiveJSONKeys[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
