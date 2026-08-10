package moonshine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTranscriptEventsMirrorSnapshotFlagsInOrder(t *testing.T) {
	started := TranscriptLine{Text: "new", IsNew: true, IsUpdated: true, HasTextChanged: true}
	updated := TranscriptLine{Text: "changing", IsUpdated: true, HasTextChanged: true, HaveSpeakersChanged: true}
	completed := TranscriptLine{Text: "done", IsUpdated: true, IsComplete: true, HaveSpeakersChanged: true}

	events := transcriptEvents(Transcript{Lines: []TranscriptLine{started, updated, completed}})

	assert.Equal(t, []TranscriptEvent{
		LineStarted{Line: started},
		LineTextChanged{Line: started},
		LineUpdated{Line: updated},
		LineTextChanged{Line: updated},
		LineSpeakersChanged{Line: updated},
		LineSpeakersChanged{Line: completed},
		LineCompleted{Line: completed},
	}, events)
}

func TestTranscriptEventsIgnoreUnchangedLines(t *testing.T) {
	events := transcriptEvents(Transcript{Lines: []TranscriptLine{{Text: "stable"}}})
	assert.Empty(t, events)
}
