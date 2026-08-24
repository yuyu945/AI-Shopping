package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactJSONMasksSensitiveKeys(t *testing.T) {
	raw := json.RawMessage(`{"authorization":"Bearer secret","profile":{"phone":"13800000000"},"query":"laptop"}`)

	got := RedactJSON(raw)

	if string(got) != `{"authorization":"[REDACTED]","profile":{"phone":"[REDACTED]"},"query":"laptop"}` {
		t.Fatalf("RedactJSON() = %s", got)
	}
}

func TestRedactJSONTruncatesLongStrings(t *testing.T) {
	got := RedactJSON(json.RawMessage(`{"text":"` + strings.Repeat("a", 520) + `"}`))
	var out map[string]string
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if len([]rune(out["text"])) != 500 {
		t.Fatalf("length=%d", len([]rune(out["text"])))
	}
}

func TestRedactJSONRejectsInvalidJSON(t *testing.T) {
	got := RedactJSON(json.RawMessage(`{"token":`))

	if string(got) != `{"redaction_error":"invalid_json"}` {
		t.Fatalf("RedactJSON() = %s", got)
	}
}
