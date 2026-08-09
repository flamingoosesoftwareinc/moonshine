package moonshine

import "github.com/moonshine-ai/moonshine/language-bindings/go/raw"

// TranscribeFlags controls optional transcription behavior.
type TranscribeFlags uint32

const (
	FlagForceUpdate  TranscribeFlags = raw.MoonshineFlagForceUpdate
	FlagSpellingMode TranscribeFlags = raw.MoonshineFlagSpellingMode
)

func combineTranscribeFlags(flags []TranscribeFlags) uint32 {
	var combined TranscribeFlags
	for _, flag := range flags {
		combined |= flag
	}
	return uint32(combined)
}
