package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxFinalRecommendationCandidates = 10
const maxRecommendationReasonRunes = 512

// ErrInvalidFinalRecommendation reports model final output outside the accepted schema.
var ErrInvalidFinalRecommendation = errors.New("INVALID_FINAL_RECOMMENDATION")

// ParseFinalRecommendations parses and validates the strict model final recommendation schema.
func ParseFinalRecommendations(raw json.RawMessage) (FinalRecommendationOutput, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var output FinalRecommendationOutput
	if err := decoder.Decode(&output); err != nil {
		return FinalRecommendationOutput{}, fmt.Errorf("%w: invalid json", ErrInvalidFinalRecommendation)
	}
	if len(output.Recommendations) == 0 || len(output.Recommendations) > maxFinalRecommendationCandidates {
		return FinalRecommendationOutput{}, ErrInvalidFinalRecommendation
	}
	ranks := make(map[uint32]struct{}, len(output.Recommendations))
	skus := make(map[uint64]struct{}, len(output.Recommendations))
	for i := range output.Recommendations {
		item := &output.Recommendations[i]
		item.Reason = strings.TrimSpace(item.Reason)
		if item.SKUID == 0 || item.RankNo == 0 || item.Reason == "" || len([]rune(item.Reason)) > maxRecommendationReasonRunes {
			return FinalRecommendationOutput{}, ErrInvalidFinalRecommendation
		}
		if _, ok := ranks[item.RankNo]; ok {
			return FinalRecommendationOutput{}, ErrInvalidFinalRecommendation
		}
		if _, ok := skus[item.SKUID]; ok {
			return FinalRecommendationOutput{}, ErrInvalidFinalRecommendation
		}
		ranks[item.RankNo] = struct{}{}
		skus[item.SKUID] = struct{}{}
	}
	return output, nil
}
