package moonshine_test

import (
	"fmt"
	"time"

	"github.com/moonshine-ai/moonshine/language-bindings/go/moonshine"
)

func ExampleTranscriber_Transcribe() {
	transcriber, err := moonshine.NewTranscriber("/models/tiny-en", moonshine.ModelArchTiny)
	if err != nil {
		return
	}
	defer transcriber.Close()

	transcript, err := transcriber.Transcribe(make([]float32, 16000), 16000)
	if err != nil {
		return
	}
	fmt.Print(transcript.String())
}

func ExampleTranscriber_NewStreamWithConfig() {
	transcriber, err := moonshine.NewTranscriber("/models/tiny-en", moonshine.ModelArchTiny)
	if err != nil {
		return
	}
	defer transcriber.Close()
	stream, err := transcriber.NewStreamWithConfig(moonshine.StreamConfig{
		UpdateInterval: 250 * time.Millisecond,
	})
	if err != nil {
		return
	}
	defer stream.Close()
	remove := stream.AddListener(func(event moonshine.TranscriptEvent) {
		fmt.Print(event.EventLine().Text)
	})
	defer remove()

	if err := stream.Start(); err != nil {
		return
	}
	if err := stream.AddAudio(make([]float32, 4000), 16000); err != nil {
		return
	}
	_ = stream.Stop()
}

func ExampleEmbeddingModel_Embed() {
	model, err := moonshine.NewEmbeddingModel(
		"/models/embeddinggemma-300m-ONNX",
		moonshine.EmbeddingModelArchGemma300M,
		"q4",
	)
	if err != nil {
		return
	}
	defer model.Close()

	lights, err := model.Embed("turn on the lights", "")
	if err != nil {
		return
	}
	lamps, err := model.Embed("switch on the lamps", "")
	if err != nil {
		return
	}
	score, err := model.Similarity(lights, lamps)
	if err == nil {
		fmt.Printf("similarity: %.2f", score)
	}
}

func ExamplePhonemizer_Phonemes() {
	phonemizer, err := moonshine.NewPhonemizerFromFiles(
		"en_us",
		nil,
		moonshine.Option{Name: "g2p_root", Value: "/models/tts"},
	)
	if err != nil {
		return
	}
	defer phonemizer.Close()

	ipa, err := phonemizer.Phonemes("Hello world")
	if err == nil {
		fmt.Print(ipa)
	}
}

func ExampleTextToSpeech_Synthesize() {
	synthesizer, err := moonshine.NewTextToSpeechFromFiles(
		"en_us",
		nil,
		moonshine.Option{Name: "g2p_root", Value: "/models/tts"},
		moonshine.Option{Name: "voice", Value: "piper_en_US-amy-low"},
	)
	if err != nil {
		return
	}
	defer synthesizer.Close()

	audio, err := synthesizer.Synthesize("Hello from Moonshine")
	if err == nil {
		fmt.Printf("%d samples at %d Hz", len(audio.Samples), audio.SampleRate)
	}
}
