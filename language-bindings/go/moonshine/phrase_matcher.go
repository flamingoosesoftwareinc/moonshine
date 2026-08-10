package moonshine

import (
	"fmt"
	"sort"
	"strings"
)

// EmbeddingBackend is the narrow AgentFlow matching boundary implemented by
// EmbeddingModel and easily replaced by a test double.
type EmbeddingBackend interface {
	Embed(text, modelName string) ([]float32, error)
	Similarity(a, b []float32) (float32, error)
}

// MatchResult reports the accepted key, if any, and the best observed score.
type MatchResult struct {
	Key   string
	Score float32
	Found bool
}

type embeddedPhrase struct {
	key       string
	embedding []float32
}

// PhraseMatcher performs semantic matching against phrase vectors computed
// once during construction.
type PhraseMatcher struct {
	backend   EmbeddingBackend
	threshold float32
	phrases   []embeddedPhrase
}

func NewPhraseMatcher(
	backend EmbeddingBackend,
	phrasesByKey map[string][]string,
	threshold float32,
) (*PhraseMatcher, error) {
	if backend == nil || threshold < -1 || threshold > 1 {
		return nil, fmt.Errorf("invalid phrase matcher configuration: %w", ErrInvalidArgument)
	}
	keys := make([]string, 0, len(phrasesByKey))
	for key := range phrasesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	matcher := &PhraseMatcher{backend: backend, threshold: threshold}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("empty phrase matcher key: %w", ErrInvalidArgument)
		}
		for _, phrase := range phrasesByKey[key] {
			if strings.TrimSpace(phrase) == "" {
				continue
			}
			embedding, err := backend.Embed(phrase, "")
			if err != nil {
				return nil, fmt.Errorf("embed phrase %q: %w", phrase, err)
			}
			matcher.phrases = append(matcher.phrases, embeddedPhrase{
				key: key, embedding: append([]float32(nil), embedding...),
			})
		}
	}
	return matcher, nil
}

func (m *PhraseMatcher) Threshold() float32 {
	if m == nil {
		return 0
	}
	return m.threshold
}

func (m *PhraseMatcher) Match(utterance string) (MatchResult, error) {
	if m == nil || m.backend == nil {
		return MatchResult{}, ErrClosed
	}
	if strings.TrimSpace(utterance) == "" {
		return MatchResult{}, nil
	}
	embedding, err := m.backend.Embed(utterance, "")
	if err != nil {
		return MatchResult{}, fmt.Errorf("embed utterance: %w", err)
	}
	result := MatchResult{Score: -1}
	for _, phrase := range m.phrases {
		score, err := m.backend.Similarity(embedding, phrase.embedding)
		if err != nil {
			return MatchResult{}, fmt.Errorf("compare phrase %q: %w", phrase.key, err)
		}
		if score > result.Score {
			result.Key = phrase.key
			result.Score = score
		}
	}
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Key != "" && result.Score >= m.threshold {
		result.Found = true
	} else {
		result.Key = ""
	}
	return result, nil
}

func (m *PhraseMatcher) MatchUtterance(utterance string) (MatchResult, error) {
	return m.Match(utterance)
}

// SubstringMatcher is the deterministic no-model fallback used for tests and
// offline flows. The longest case-insensitive containment match wins.
type SubstringMatcher struct {
	threshold float32
	groups    []substringGroup
}

type substringGroup struct {
	key     string
	phrases []string
}

func NewSubstringMatcher(
	phrasesByKey map[string][]string,
	threshold float32,
) (*SubstringMatcher, error) {
	if threshold < 0 || threshold > 1 {
		return nil, fmt.Errorf("invalid substring threshold: %w", ErrInvalidArgument)
	}
	keys := make([]string, 0, len(phrasesByKey))
	for key := range phrasesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	matcher := &SubstringMatcher{threshold: threshold}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("empty substring matcher key: %w", ErrInvalidArgument)
		}
		group := substringGroup{key: key}
		for _, phrase := range phrasesByKey[key] {
			phrase = strings.ToLower(strings.TrimSpace(phrase))
			if phrase != "" {
				group.phrases = append(group.phrases, phrase)
			}
		}
		matcher.groups = append(matcher.groups, group)
	}
	return matcher, nil
}

func (m *SubstringMatcher) Threshold() float32 {
	if m == nil {
		return 0
	}
	return m.threshold
}

func (m *SubstringMatcher) Match(utterance string) MatchResult {
	if m == nil {
		return MatchResult{}
	}
	text := strings.ToLower(strings.TrimSpace(utterance))
	if text == "" {
		return MatchResult{}
	}
	result := MatchResult{}
	bestLength := 0
	for _, group := range m.groups {
		for _, phrase := range group.phrases {
			if (strings.Contains(text, phrase) || strings.Contains(phrase, text)) && len(phrase) > bestLength {
				bestLength = len(phrase)
				result.Key = group.key
			}
		}
	}
	if result.Key == "" {
		return result
	}
	result.Score = min(1, float32(bestLength)/float32(max(len(text), 1)))
	result.Found = true
	return result
}

func (m *SubstringMatcher) MatchUtterance(utterance string) (MatchResult, error) {
	return m.Match(utterance), nil
}
