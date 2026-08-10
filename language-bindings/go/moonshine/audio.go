package moonshine

// Audio is mono float-PCM audio produced by Moonshine.
type Audio struct {
	Samples    []float32
	SampleRate int
}
