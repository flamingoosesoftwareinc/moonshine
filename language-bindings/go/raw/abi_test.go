package raw_test

import (
	"testing"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
	"github.com/stretchr/testify/assert"
)

// Keeping every generated function in one compile-time inventory makes a
// header or generator change fail loudly instead of silently dropping part of
// the C API from the Go binding.
var generatedFunctions = []any{
	raw.MoonshineGetVersion,
	raw.MoonshineErrorToString,
	raw.MoonshineFreeBuffer,
	raw.MoonshineTranscriptToString,
	raw.MoonshineLoadTranscriberFromFiles,
	raw.MoonshineLoadTranscriberFromMemory,
	raw.MoonshineLoadTranscriberFromMemoryFiles,
	raw.MoonshineFreeTranscriber,
	raw.MoonshineTranscribeWithoutStreaming,
	raw.MoonshineCreateStream,
	raw.MoonshineFreeStream,
	raw.MoonshineStartStream,
	raw.MoonshineStopStream,
	raw.MoonshineTranscribeAddAudioToStream,
	raw.MoonshineTranscribeStream,
	raw.MoonshineCreateEmbeddingModel,
	raw.MoonshineCreateEmbeddingModelFromMemory,
	raw.MoonshineFreeEmbeddingModel,
	raw.MoonshineCalculateEmbedding,
	raw.MoonshineFreeEmbedding,
	raw.MoonshineCalculateEmbeddingDistance,
	raw.MoonshineExtractSpeechClip,
	raw.MoonshineCreateTtsSynthesizerFromFiles,
	raw.MoonshineCreateTtsSynthesizerFromMemory,
	raw.MoonshineFreeTtsSynthesizer,
	raw.MoonshineGetG2pDependencies,
	raw.MoonshineGetTtsDependencies,
	raw.MoonshineGetTtsVoices,
	raw.MoonshineGetSttDependencies,
	raw.MoonshineGetEmbeddingDependencies,
	raw.MoonshineGetDiarizationDependencies,
	raw.MoonshineGetSttCatalog,
	raw.MoonshineGetEmbeddingCatalog,
	raw.MoonshineTextToSpeech,
	raw.MoonshinePhonemesToSpeech,
	raw.MoonshineCreateGraphemeToPhonemizerFromFiles,
	raw.MoonshineCreateGraphemeToPhonemizerFromMemory,
	raw.MoonshineFreeGraphemeToPhonemizer,
	raw.MoonshineTextToPhonemes,
}

func TestGeneratedFunctionInventory(t *testing.T) {
	assert.Len(t, generatedFunctions, 39)
	for _, function := range generatedFunctions {
		assert.NotNil(t, function)
	}
}

func TestGeneratedConstantsMatchCAPISemantics(t *testing.T) {
	assert.Equal(t, 30000, raw.MoonshineHeaderVersion)
	assert.Equal(t, 30000, raw.MoonshineFromMemoryRemovedVersion)

	assert.Equal(t, 0, raw.MoonshineModelArchTiny)
	assert.Equal(t, 1, raw.MoonshineModelArchBase)
	assert.Equal(t, 2, raw.MoonshineModelArchTinyStreaming)
	assert.Equal(t, 3, raw.MoonshineModelArchBaseStreaming)
	assert.Equal(t, 4, raw.MoonshineModelArchSmallStreaming)
	assert.Equal(t, 5, raw.MoonshineModelArchMediumStreaming)
	assert.Equal(t, 0, raw.MoonshineEmbeddingModelArchGemma300m)

	assert.Equal(t, 0, raw.MoonshineErrorNone)
	assert.Equal(t, -1, raw.MoonshineErrorUnknown)
	assert.Equal(t, -2, raw.MoonshineErrorInvalidHandle)
	assert.Equal(t, -3, raw.MoonshineErrorInvalidArgument)

	assert.Equal(t, 1<<0, raw.MoonshineFlagForceUpdate)
	assert.Equal(t, 1<<1, raw.MoonshineFlagSpellingMode)
}

func TestGeneratedStructFieldsRemainAvailable(t *testing.T) {
	option := raw.MoonshineOptionT{}
	option.Name = []byte("name")
	option.Value = []byte("value")

	word := raw.TranscriptWordT{}
	word.Text = []byte("word")
	word.Start = 1
	word.End = 2
	word.Confidence = 0.9

	span := raw.SpeakerSpanT{}
	span.StartTime = 1
	span.Duration = 2
	span.SpeakerId = 3
	span.SpeakerIndex = 4
	span.StartChar = 5
	span.EndChar = 6

	line := raw.TranscriptLineT{}
	line.Text = []byte("line")
	line.AudioData = []float32{0.25}
	line.AudioDataCount = 1
	line.StartTime = 1
	line.Duration = 2
	line.Id = 3
	line.IsComplete = 1
	line.IsUpdated = 1
	line.IsNew = 1
	line.HasTextChanged = 1
	line.HaveSpeakersChanged = 1
	line.SpeakerSpans = []raw.SpeakerSpanT{span}
	line.SpeakerSpanCount = 1
	line.LastTranscriptionLatencyMs = 4
	line.Words = []raw.TranscriptWordT{word}
	line.WordCount = 1

	transcript := raw.TranscriptT{}
	transcript.Lines = []raw.TranscriptLineT{line}
	transcript.LineCount = 1

	clip := raw.MoonshineSpeechClipT{}
	clip.AudioData = []float32{0.5}
	clip.AudioLength = 1
	clip.StartTime = 1
	clip.SpeechDuration = 2
	clip.IsComplete = 1
	clip.Transcript = []byte("clip")

	assert.Equal(t, []byte("name"), option.Name)
	assert.Equal(t, uint64(1), transcript.LineCount)
	assert.Equal(t, []byte("clip"), clip.Transcript)
}
