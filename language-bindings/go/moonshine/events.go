package moonshine

// TranscriptEvent is emitted from the change flags on a stream snapshot.
// Event values and their lines are entirely Go-owned.
type TranscriptEvent interface {
	EventLine() TranscriptLine
	isTranscriptEvent()
}

// LineStarted indicates that a line appeared for the first time.
type LineStarted struct{ Line TranscriptLine }

func (e LineStarted) EventLine() TranscriptLine { return e.Line }
func (LineStarted) isTranscriptEvent()          {}

// LineUpdated indicates that an existing incomplete line was updated.
type LineUpdated struct{ Line TranscriptLine }

func (e LineUpdated) EventLine() TranscriptLine { return e.Line }
func (LineUpdated) isTranscriptEvent()          {}

// LineTextChanged indicates that a line's recognized text changed.
type LineTextChanged struct{ Line TranscriptLine }

func (e LineTextChanged) EventLine() TranscriptLine { return e.Line }
func (LineTextChanged) isTranscriptEvent()          {}

// LineSpeakersChanged indicates that diarization revised a line's speakers.
type LineSpeakersChanged struct{ Line TranscriptLine }

func (e LineSpeakersChanged) EventLine() TranscriptLine { return e.Line }
func (LineSpeakersChanged) isTranscriptEvent()          {}

// LineCompleted indicates that an updated line became final.
type LineCompleted struct{ Line TranscriptLine }

func (e LineCompleted) EventLine() TranscriptLine { return e.Line }
func (LineCompleted) isTranscriptEvent()          {}

func transcriptEvents(transcript Transcript) []TranscriptEvent {
	events := make([]TranscriptEvent, 0)
	for _, line := range transcript.Lines {
		if line.IsNew {
			events = append(events, LineStarted{Line: line})
		}
		if line.IsUpdated && !line.IsNew && !line.IsComplete {
			events = append(events, LineUpdated{Line: line})
		}
		if line.HasTextChanged {
			events = append(events, LineTextChanged{Line: line})
		}
		if line.HaveSpeakersChanged {
			events = append(events, LineSpeakersChanged{Line: line})
		}
		if line.IsComplete && line.IsUpdated {
			events = append(events, LineCompleted{Line: line})
		}
	}
	return events
}
