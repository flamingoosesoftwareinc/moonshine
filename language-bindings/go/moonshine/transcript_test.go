package moonshine

import (
	"testing"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyRawTranscript(t *testing.T) {
	source := raw.TranscriptT{
		Lines: []raw.TranscriptLineT{{
			Text:                       []byte("hello 世界\x00ignored"),
			AudioData:                  []float32{0.25, -0.5},
			AudioDataCount:             2,
			StartTime:                  1.25,
			Duration:                   2.5,
			Id:                         99,
			IsComplete:                 1,
			IsUpdated:                  1,
			IsNew:                      1,
			HasTextChanged:             1,
			HaveSpeakersChanged:        1,
			LastTranscriptionLatencyMs: 42,
			SpeakerSpans: []raw.SpeakerSpanT{{
				StartTime: 1, SpeakerId: 7, SpeakerIndex: 2,
				Duration: 0.5, StartChar: 0, EndChar: 5,
			}},
			SpeakerSpanCount: 1,
			Words: []raw.TranscriptWordT{{
				Text: []byte("hello\x00"), Start: 1.25, End: 1.75, Confidence: 0.95,
			}},
			WordCount: 1,
		}},
		LineCount: 1,
	}

	transcript, err := copyRawTranscript(source)
	require.NoError(t, err)
	require.Len(t, transcript.Lines, 1)

	line := transcript.Lines[0]
	assert.Equal(t, "hello 世界", line.Text)
	assert.Equal(t, []float32{0.25, -0.5}, line.AudioData)
	assert.Equal(t, float32(1.25), line.StartTime)
	assert.Equal(t, float32(2.5), line.Duration)
	assert.Equal(t, uint64(99), line.ID)
	assert.True(t, line.IsComplete)
	assert.True(t, line.IsUpdated)
	assert.True(t, line.IsNew)
	assert.True(t, line.HasTextChanged)
	assert.True(t, line.HaveSpeakersChanged)
	assert.Equal(t, uint32(42), line.LastTranscriptionLatencyMS)
	assert.Equal(t, []SpeakerSpan{{
		StartTime: 1, Duration: 0.5, SpeakerID: 7, SpeakerIndex: 2, StartChar: 0, EndChar: 5,
	}}, line.SpeakerSpans)
	assert.Equal(t, []WordTiming{{
		Word: "hello", Start: 1.25, End: 1.75, Confidence: 0.95,
	}}, line.Words)

	source.Lines[0].Text[0] = 'X'
	source.Lines[0].AudioData[0] = 1
	source.Lines[0].Words[0].Text[0] = 'X'
	assert.Equal(t, "hello 世界", line.Text)
	assert.Equal(t, float32(0.25), line.AudioData[0])
	assert.Equal(t, "hello", line.Words[0].Word)
}

func TestCopyRawTranscriptEmpty(t *testing.T) {
	transcript, err := copyRawTranscript(raw.TranscriptT{})

	require.NoError(t, err)
	assert.Empty(t, transcript.Lines)
}

func TestCopyRawTranscriptRejectsInconsistentCounts(t *testing.T) {
	tests := []struct {
		name       string
		transcript raw.TranscriptT
	}{
		{name: "lines", transcript: raw.TranscriptT{LineCount: 1}},
		{name: "audio", transcript: raw.TranscriptT{
			Lines: []raw.TranscriptLineT{{AudioDataCount: 1}}, LineCount: 1,
		}},
		{name: "spans", transcript: raw.TranscriptT{
			Lines: []raw.TranscriptLineT{{SpeakerSpanCount: 1}}, LineCount: 1,
		}},
		{name: "words", transcript: raw.TranscriptT{
			Lines: []raw.TranscriptLineT{{WordCount: 1}}, LineCount: 1,
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := copyRawTranscript(test.transcript)
			require.ErrorIs(t, err, ErrInvalidArgument)
		})
	}
}

func TestTranscriptString(t *testing.T) {
	transcript := Transcript{Lines: []TranscriptLine{{
		Text: "hello", StartTime: 1, Duration: 2, ID: 3, IsComplete: true,
	}}}

	assert.Contains(t, transcript.String(), "hello")
	assert.Contains(t, transcript.String(), "complete: true")
}
