package moonshine

import (
	"bytes"
	"fmt"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

func copyRawTranscript(source raw.TranscriptT) (Transcript, error) {
	lineCount, err := checkedNativeCount("transcript lines", source.LineCount, len(source.Lines))
	if err != nil {
		return Transcript{}, err
	}

	result := Transcript{Lines: make([]TranscriptLine, lineCount)}
	for index := range lineCount {
		line, err := copyRawTranscriptLine(source.Lines[index])
		if err != nil {
			return Transcript{}, fmt.Errorf("transcript line %d: %w", index, err)
		}
		result.Lines[index] = line
	}
	return result, nil
}

func copyRawTranscriptLine(source raw.TranscriptLineT) (TranscriptLine, error) {
	audioCount, err := checkedNativeCount("audio samples", source.AudioDataCount, len(source.AudioData))
	if err != nil {
		return TranscriptLine{}, err
	}
	spanCount, err := checkedNativeCount("speaker spans", source.SpeakerSpanCount, len(source.SpeakerSpans))
	if err != nil {
		return TranscriptLine{}, err
	}
	wordCount, err := checkedNativeCount("words", source.WordCount, len(source.Words))
	if err != nil {
		return TranscriptLine{}, err
	}

	result := TranscriptLine{
		Text:                       copyRawText(source.Text),
		AudioData:                  append([]float32(nil), source.AudioData[:audioCount]...),
		StartTime:                  source.StartTime,
		Duration:                   source.Duration,
		ID:                         source.Id,
		IsComplete:                 source.IsComplete != 0,
		IsUpdated:                  source.IsUpdated != 0,
		IsNew:                      source.IsNew != 0,
		HasTextChanged:             source.HasTextChanged != 0,
		HaveSpeakersChanged:        source.HaveSpeakersChanged != 0,
		SpeakerSpans:               make([]SpeakerSpan, spanCount),
		Words:                      make([]WordTiming, wordCount),
		LastTranscriptionLatencyMS: source.LastTranscriptionLatencyMs,
	}

	for index := range spanCount {
		span := source.SpeakerSpans[index]
		result.SpeakerSpans[index] = SpeakerSpan{
			StartTime:    span.StartTime,
			Duration:     span.Duration,
			SpeakerID:    span.SpeakerId,
			SpeakerIndex: span.SpeakerIndex,
			StartChar:    span.StartChar,
			EndChar:      span.EndChar,
		}
	}
	for index := range wordCount {
		word := source.Words[index]
		result.Words[index] = WordTiming{
			Word:       copyRawText(word.Text),
			Start:      word.Start,
			End:        word.End,
			Confidence: word.Confidence,
		}
	}

	return result, nil
}

func checkedNativeCount(name string, count uint64, available int) (int, error) {
	if count > uint64(available) {
		return 0, fmt.Errorf(
			"native %s count %d exceeds available values %d: %w",
			name,
			count,
			available,
			ErrInvalidArgument,
		)
	}
	return int(count), nil
}

func copyRawText(value []byte) string {
	if nul := bytes.IndexByte(value, 0); nul >= 0 {
		value = value[:nul]
	}
	return string(value)
}
