// Command voice-roundtrip synthesizes a known phrase, writes it as a WAV, and
// immediately transcribes the same samples. It is an explicit model-backed
// confidence check; run it through scripts/build-go.sh roundtrip.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/moonshine-ai/moonshine/language-bindings/go/moonshine"
)

const phrase = "The best of times and the worst of times."

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "voice round-trip:", err)
		os.Exit(1)
	}
}

func run() error {
	sttModel := flag.String("stt-model", "", "path to a Tiny English model directory")
	ttsRoot := flag.String("tts-root", "", "path containing Moonshine TTS assets")
	output := flag.String("output", "voice-roundtrip.wav", "output WAV path")
	flag.Parse()
	if *sttModel == "" || *ttsRoot == "" {
		return errors.New("both -stt-model and -tts-root are required")
	}

	synthesizer, err := moonshine.NewTextToSpeechFromFiles(
		"en_us",
		nil,
		moonshine.Option{Name: "g2p_root", Value: *ttsRoot},
		moonshine.Option{Name: "voice", Value: "piper_en_US-amy-low"},
	)
	if err != nil {
		return fmt.Errorf("create synthesizer: %w", err)
	}
	defer synthesizer.Close()

	audio, err := synthesizer.Synthesize(phrase)
	if err != nil {
		return fmt.Errorf("synthesize: %w", err)
	}
	if len(audio.Samples) == 0 || audio.SampleRate <= 0 {
		return fmt.Errorf("synthesis returned %d samples at %d Hz", len(audio.Samples), audio.SampleRate)
	}
	if err := writeWAV(*output, audio); err != nil {
		return fmt.Errorf("write WAV: %w", err)
	}

	transcriber, err := moonshine.NewTranscriber(*sttModel, moonshine.ModelArchTiny)
	if err != nil {
		return fmt.Errorf("create transcriber: %w", err)
	}
	defer transcriber.Close()

	transcript, err := transcriber.Transcribe(audio.Samples, audio.SampleRate)
	if err != nil {
		return fmt.Errorf("transcribe synthesized audio: %w", err)
	}
	recognized := strings.TrimSpace(transcript.String())
	if !strings.Contains(strings.ToLower(recognized), "best of times") ||
		!strings.Contains(strings.ToLower(recognized), "worst of times") {
		return fmt.Errorf("unexpected transcript %q", recognized)
	}

	fmt.Printf("synthesized: %q\n", phrase)
	fmt.Printf("transcribed: %q\n", recognized)
	fmt.Printf("audio: %s (%d samples at %d Hz)\n", *output, len(audio.Samples), audio.SampleRate)
	return nil
}

func writeWAV(path string, audio moonshine.Audio) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	dataSize := uint32(len(audio.Samples) * 2)
	if _, err = io.WriteString(file, "RIFF"); err != nil {
		return err
	}
	fields := []any{
		uint32(36) + dataSize,
		[4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '},
		uint32(16), uint16(1), uint16(1), uint32(audio.SampleRate),
		uint32(audio.SampleRate * 2), uint16(2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, dataSize,
	}
	for _, field := range fields {
		if err = binary.Write(file, binary.LittleEndian, field); err != nil {
			return err
		}
	}
	for _, sample := range audio.Samples {
		clamped := max(-1, min(1, float64(sample)))
		if err = binary.Write(file, binary.LittleEndian, int16(math.Round(clamped*32767))); err != nil {
			return err
		}
	}
	return nil
}
