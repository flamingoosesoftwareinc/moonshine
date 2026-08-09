package moonshine

import (
	"bytes"
	"fmt"
	"unsafe"

	"github.com/moonshine-ai/moonshine/language-bindings/go/raw"
)

func materializeRawTranscript(source raw.TranscriptT) (raw.TranscriptT, error) {
	source.Deref()
	lineCount, err := nativeSliceLength("transcript lines", source.LineCount)
	if err != nil {
		return raw.TranscriptT{}, err
	}
	source.Lines = make([]raw.TranscriptLineT, lineCount)
	source.Deref()

	for lineIndex := range source.Lines {
		line := &source.Lines[lineIndex]
		line.Deref()

		audioCount, err := nativeSliceLength("audio samples", line.AudioDataCount)
		if err != nil {
			return raw.TranscriptT{}, fmt.Errorf("transcript line %d: %w", lineIndex, err)
		}
		spanCount, err := nativeSliceLength("speaker spans", line.SpeakerSpanCount)
		if err != nil {
			return raw.TranscriptT{}, fmt.Errorf("transcript line %d: %w", lineIndex, err)
		}
		wordCount, err := nativeSliceLength("words", line.WordCount)
		if err != nil {
			return raw.TranscriptT{}, fmt.Errorf("transcript line %d: %w", lineIndex, err)
		}

		text := copyCString(unsafe.SliceData(line.Text))
		line.Text = make([]byte, len(text))
		line.AudioData = nativeFloat32Slice(unsafe.SliceData(line.AudioData), audioCount)
		line.SpeakerSpans = make([]raw.SpeakerSpanT, spanCount)
		line.Words = make([]raw.TranscriptWordT, wordCount)
		line.Deref()

		for spanIndex := range line.SpeakerSpans {
			line.SpeakerSpans[spanIndex].Deref()
		}
		for wordIndex := range line.Words {
			word := &line.Words[wordIndex]
			word.Deref()
			word.Text = []byte(copyCString(unsafe.SliceData(word.Text)))
		}
	}
	return source, nil
}

func nativeSliceLength(name string, count uint64) (int, error) {
	maxInt := uint64(^uint(0) >> 1)
	if count > maxInt {
		return 0, fmt.Errorf("native %s count %d overflows int: %w", name, count, ErrInvalidArgument)
	}
	return int(count), nil
}

func nativeFloat32Slice(pointer *float32, length int) []float32 {
	if length == 0 {
		return nil
	}
	return unsafe.Slice(pointer, length)
}

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
