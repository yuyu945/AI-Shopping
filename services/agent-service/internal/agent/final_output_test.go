package agent

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseFinalRecommendationsAcceptsValidOutput(t *testing.T) {
	got, err := ParseFinalRecommendations(json.RawMessage(`{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"适合编程"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Recommendations) != 1 || got.Recommendations[0].SKUID != 2001 || got.Recommendations[0].RankNo != 1 {
		t.Fatalf("got=%#v", got)
	}
}

func TestParseFinalRecommendationsRejectsForgedFields(t *testing.T) {
	_, err := ParseFinalRecommendations(json.RawMessage(`{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"ok","price":"0.01"}]}`))
	if !errors.Is(err, ErrInvalidFinalRecommendation) {
		t.Fatalf("error=%v, want ErrInvalidFinalRecommendation", err)
	}
}

func TestParseFinalRecommendationsRejectsInvalidShape(t *testing.T) {
	tests := map[string]string{
		"empty":        `{"recommendations":[]}`,
		"zero sku":     `{"recommendations":[{"sku_id":0,"rank_no":1,"reason":"ok"}]}`,
		"zero rank":    `{"recommendations":[{"sku_id":2001,"rank_no":0,"reason":"ok"}]}`,
		"blank reason": `{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"   "}]}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFinalRecommendations(json.RawMessage(raw))
			if !errors.Is(err, ErrInvalidFinalRecommendation) {
				t.Fatalf("error=%v, want ErrInvalidFinalRecommendation", err)
			}
		})
	}
}

func TestParseFinalRecommendationsRejectsDuplicateRankOrSKU(t *testing.T) {
	tests := map[string]string{
		"rank": `{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"a"},{"sku_id":2002,"rank_no":1,"reason":"b"}]}`,
		"sku":  `{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"a"},{"sku_id":2001,"rank_no":2,"reason":"b"}]}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFinalRecommendations(json.RawMessage(raw))
			if !errors.Is(err, ErrInvalidFinalRecommendation) {
				t.Fatalf("error=%v, want ErrInvalidFinalRecommendation", err)
			}
		})
	}
}

func TestParseFinalRecommendationsCapsCandidates(t *testing.T) {
	raw := `{"recommendations":[
		{"sku_id":1,"rank_no":1,"reason":"a"},{"sku_id":2,"rank_no":2,"reason":"b"},
		{"sku_id":3,"rank_no":3,"reason":"c"},{"sku_id":4,"rank_no":4,"reason":"d"},
		{"sku_id":5,"rank_no":5,"reason":"e"},{"sku_id":6,"rank_no":6,"reason":"f"},
		{"sku_id":7,"rank_no":7,"reason":"g"},{"sku_id":8,"rank_no":8,"reason":"h"},
		{"sku_id":9,"rank_no":9,"reason":"i"},{"sku_id":10,"rank_no":10,"reason":"j"},
		{"sku_id":11,"rank_no":11,"reason":"k"}
	]}`
	_, err := ParseFinalRecommendations(json.RawMessage(raw))
	if !errors.Is(err, ErrInvalidFinalRecommendation) {
		t.Fatalf("error=%v, want ErrInvalidFinalRecommendation", err)
	}
}
