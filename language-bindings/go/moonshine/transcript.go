package moonshine

import (
	"fmt"
	"strings"
)

// WordTiming is one transcribed word with model timing and confidence.
type WordTiming struct {
	Word       string
	Start      float32
	End        float32
	Confidence float32
}

// SpeakerSpan attributes a contiguous portion of a line to one speaker.
type SpeakerSpan struct {
	StartTime    float32
	Duration     float32
	SpeakerID    uint64
	SpeakerIndex uint32
	StartChar    uint64
	EndChar      uint64
}

// TranscriptLine is one phrase or sentence in a transcript.
type TranscriptLine struct {
	Text                       string
	AudioData                  []float32
	StartTime                  float32
	Duration                   float32
	ID                         uint64
	IsComplete                 bool
	IsUpdated                  bool
	IsNew                      bool
	HasTextChanged             bool
	HaveSpeakersChanged        bool
	SpeakerSpans               []SpeakerSpan
	Words                      []WordTiming
	LastTranscriptionLatencyMS uint32
}

func (line TranscriptLine) String() string {
	return fmt.Sprintf(
		"TranscriptLine(text: %q, startTime: %g, duration: %g, id: %d, complete: %t)",
		line.Text,
		line.StartTime,
		line.Duration,
		line.ID,
		line.IsComplete,
	)
}

// Transcript is a Go-owned snapshot of all transcription lines.
type Transcript struct {
	Lines []TranscriptLine
}

func (transcript Transcript) String() string {
	var result strings.Builder
	result.WriteString("Transcript(")
	for index, line := range transcript.Lines {
		if index > 0 {
			result.WriteString("\n")
		}
		result.WriteString(line.String())
	}
	result.WriteString(")")
	return result.String()
}
