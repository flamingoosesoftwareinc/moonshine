package moonshine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEmbeddingBackend struct {
	vectors       map[string][]float32
	embedCalls    []string
	similarities  map[[2]float32]float32
	embedErr      error
	similarityErr error
}

func (f *fakeEmbeddingBackend) Embed(text, _ string) ([]float32, error) {
	f.embedCalls = append(f.embedCalls, text)
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	return append([]float32(nil), f.vectors[text]...), nil
}
func (f *fakeEmbeddingBackend) Similarity(a, b []float32) (float32, error) {
	if f.similarityErr != nil {
		return 0, f.similarityErr
	}
	return f.similarities[[2]float32{a[0], b[0]}], nil
}

func TestPhraseMatcherCachesPhrasesAndSelectsBestAboveThreshold(t *testing.T) {
	backend := &fakeEmbeddingBackend{
		vectors: map[string][]float32{
			"yes": {1}, "sure": {2}, "no": {3}, "please proceed": {4},
		},
		similarities: map[[2]float32]float32{
			{4, 1}: 0.5, {4, 2}: 0.9, {4, 3}: 0.1,
		},
	}
	matcher, err := NewPhraseMatcher(backend, map[string][]string{
		"yes": {"yes", "sure"}, "no": {"no"},
	}, 0.6)
	require.NoError(t, err)
	assert.Equal(t, []string{"no", "yes", "sure"}, backend.embedCalls)

	result, err := matcher.Match("please proceed")

	require.NoError(t, err)
	assert.Equal(t, MatchResult{Key: "yes", Score: 0.9, Found: true}, result)
	assert.Equal(t, float32(0.6), matcher.Threshold())
}

func TestPhraseMatcherReportsBestScoreBelowThreshold(t *testing.T) {
	backend := &fakeEmbeddingBackend{
		vectors:      map[string][]float32{"yes": {1}, "maybe": {2}},
		similarities: map[[2]float32]float32{{2, 1}: 0.4},
	}
	matcher, err := NewPhraseMatcher(backend, map[string][]string{"yes": {"yes"}}, 0.6)
	require.NoError(t, err)
	result, err := matcher.Match("maybe")
	require.NoError(t, err)
	assert.Equal(t, MatchResult{Score: 0.4}, result)
}

func TestPhraseMatcherHandlesEmptyAndErrors(t *testing.T) {
	backend := &fakeEmbeddingBackend{vectors: map[string][]float32{"yes": {1}}}
	matcher, err := NewPhraseMatcher(backend, map[string][]string{"yes": {"", "yes"}}, 0)
	require.NoError(t, err)
	result, err := matcher.Match("")
	require.NoError(t, err)
	assert.Equal(t, MatchResult{}, result)

	embedErr := errors.New("embed")
	backend.embedErr = embedErr
	_, err = matcher.Match("hello")
	require.ErrorIs(t, err, embedErr)
	backend.embedErr = nil
	backend.vectors["hello"] = []float32{2}
	similarityErr := errors.New("similarity")
	backend.similarityErr = similarityErr
	_, err = matcher.Match("hello")
	require.ErrorIs(t, err, similarityErr)
}

func TestPhraseMatcherRejectsInvalidConfiguration(t *testing.T) {
	_, err := NewPhraseMatcher(nil, nil, 0.5)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = NewPhraseMatcher(&fakeEmbeddingBackend{}, nil, 2)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = NewPhraseMatcher(&fakeEmbeddingBackend{}, map[string][]string{"": {"x"}}, 0.5)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestSubstringMatcherUsesLongestContainmentMatch(t *testing.T) {
	matcher, err := NewSubstringMatcher(map[string][]string{
		"lights": {"lights"}, "off": {"turn off the lights"},
	}, 0.55)
	require.NoError(t, err)
	result := matcher.Match("please turn off the lights")
	assert.Equal(t, "off", result.Key)
	assert.True(t, result.Found)
	assert.InDelta(t, float32(len("turn off the lights"))/float32(len("please turn off the lights")), result.Score, 0.0001)
	assert.Equal(t, float32(0.55), matcher.Threshold())
	assert.Equal(t, MatchResult{}, matcher.Match("open garage"))
}

func TestSubstringMatcherRejectsInvalidConfiguration(t *testing.T) {
	_, err := NewSubstringMatcher(nil, -1)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = NewSubstringMatcher(map[string][]string{"": {"x"}}, 0.5)
	require.ErrorIs(t, err, ErrInvalidArgument)
}
