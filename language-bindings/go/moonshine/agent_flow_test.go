package moonshine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAgentInput struct {
	mu            sync.Mutex
	listener      func(TranscriptEvent)
	errorListener func(error)
	startCalls    int
	stopCalls     int
	closeCalls    int
	startErr      error
	stopErr       error
	closeErr      error
}

func (f *fakeAgentInput) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	return f.startErr
}
func (f *fakeAgentInput) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	return f.stopErr
}
func (f *fakeAgentInput) AddListener(listener func(TranscriptEvent)) func() {
	f.mu.Lock()
	f.listener = listener
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.listener = nil
		f.mu.Unlock()
	}
}
func (f *fakeAgentInput) AddErrorListener(listener func(error)) func() {
	f.mu.Lock()
	f.errorListener = listener
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.errorListener = nil
		f.mu.Unlock()
	}
}
func (f *fakeAgentInput) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	return f.closeErr
}
func (f *fakeAgentInput) emit(event TranscriptEvent) {
	f.mu.Lock()
	listener := f.listener
	f.mu.Unlock()
	if listener != nil {
		listener(event)
	}
}
func (f *fakeAgentInput) emitError(err error) {
	f.mu.Lock()
	listener := f.errorListener
	f.mu.Unlock()
	if listener != nil {
		listener(err)
	}
}

type fakeAgentSpeaker struct {
	texts      []string
	options    [][]Option
	audio      Audio
	err        error
	closeCalls int
}

func (f *fakeAgentSpeaker) Synthesize(text string, options ...Option) (Audio, error) {
	f.texts = append(f.texts, text)
	f.options = append(f.options, append([]Option(nil), options...))
	return f.audio, f.err
}
func (f *fakeAgentSpeaker) Close() error { f.closeCalls++; return nil }

type fakeAudioOutput struct {
	audio      []Audio
	err        error
	closeCalls int
}

func (f *fakeAudioOutput) Play(_ context.Context, audio Audio) error {
	f.audio = append(f.audio, audio)
	return f.err
}
func (f *fakeAudioOutput) Close() error { f.closeCalls++; return nil }

func TestAgentFlowRoutesTriggersOtherwiseAndCallbacks(t *testing.T) {
	flow, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	var handled, otherwise []string
	require.NoError(t, flow.Register("lights", []string{"turn on the lights"}, func(_ context.Context, utterance string) error {
		handled = append(handled, utterance)
		return nil
	}))
	flow.Otherwise(func(_ context.Context, utterance string) error {
		otherwise = append(otherwise, utterance)
		return nil
	})
	var heard []string
	remove := flow.OnHeard(func(text string) { heard = append(heard, text) })

	found, err := flow.HandleUtterance(context.Background(), "please turn on the lights")
	require.NoError(t, err)
	assert.True(t, found)
	found, err = flow.HandleUtterance(context.Background(), "open the garage")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, []string{"please turn on the lights"}, handled)
	assert.Equal(t, []string{"open the garage"}, otherwise)
	assert.Equal(t, []string{"please turn on the lights", "open the garage"}, heard)
	remove()
	assert.True(t, flow.Unregister("lights"))
	assert.False(t, flow.Unregister("lights"))
}

func TestAgentFlowUsesEmbeddingMatcherAndCachesPhraseVectors(t *testing.T) {
	backend := &fakeEmbeddingBackend{
		vectors:      map[string][]float32{"activate": {1}, "please": {2}},
		similarities: map[[2]float32]float32{{2, 1}: 0.9},
	}
	flow, err := NewAgentFlow(
		AgentFlowResources{Embeddings: backend},
		AgentFlowConfig{UseEmbeddings: true, Threshold: 0.6},
	)
	require.NoError(t, err)
	called := 0
	require.NoError(t, flow.Register("go", []string{"activate"}, func(context.Context, string) error {
		called++
		return nil
	}))

	for range 2 {
		found, err := flow.HandleUtterance(context.Background(), "please")
		require.NoError(t, err)
		assert.True(t, found)
	}
	assert.Equal(t, 2, called)
	assert.Equal(t, []string{"activate", "please", "please"}, backend.embedCalls)
}

func TestAgentFlowSaySynthesizesPlaysAndReports(t *testing.T) {
	want := Audio{Samples: []float32{0.1}, SampleRate: 24000}
	speaker := &fakeAgentSpeaker{audio: want}
	output := &fakeAudioOutput{}
	flow, err := NewAgentFlow(
		AgentFlowResources{Speaker: speaker, Output: output}, AgentFlowConfig{},
	)
	require.NoError(t, err)
	var said []string
	flow.OnSaid(func(text string) { said = append(said, text) })
	options := []Option{{Name: "speed", Value: "1.2"}}

	require.NoError(t, flow.Say(context.Background(), "hello", options...))
	assert.Equal(t, []string{"hello"}, speaker.texts)
	assert.Equal(t, [][]Option{options}, speaker.options)
	assert.Equal(t, []Audio{want}, output.audio)
	assert.Equal(t, []string{"hello"}, said)
}

func TestAgentFlowReportsHandlerAndSpeechErrors(t *testing.T) {
	handlerErr := errors.New("handler")
	flow, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	require.NoError(t, flow.Register("go", []string{"go"}, func(context.Context, string) error {
		return handlerErr
	}))
	var seen []error
	flow.OnError(func(err error) { seen = append(seen, err) })
	_, err = flow.HandleUtterance(context.Background(), "go")
	require.ErrorIs(t, err, handlerErr)
	require.ErrorIs(t, seen[0], handlerErr)

	speechErr := errors.New("synthesis")
	flow.resources.Speaker = &fakeAgentSpeaker{err: speechErr}
	flow.resources.Output = &fakeAudioOutput{}
	err = flow.Say(context.Background(), "hello")
	require.ErrorIs(t, err, speechErr)
	require.ErrorIs(t, seen[1], speechErr)
}

func TestAgentFlowInputCallbacksAreNonBlockingAndProcessedSerially(t *testing.T) {
	input := &fakeAgentInput{}
	flow, err := NewAgentFlow(AgentFlowResources{Input: input}, AgentFlowConfig{})
	require.NoError(t, err)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	var utterances []string
	require.NoError(t, flow.Register("go", []string{"go"}, func(_ context.Context, utterance string) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		mu.Lock()
		utterances = append(utterances, utterance)
		mu.Unlock()
		return nil
	}))
	require.NoError(t, flow.Start(context.Background()))
	input.emit(LineCompleted{Line: TranscriptLine{Text: "go one"}})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	started := time.Now()
	input.emit(LineCompleted{Line: TranscriptLine{Text: "go two"}})
	assert.Less(t, time.Since(started), 50*time.Millisecond)
	close(release)
	require.NoError(t, flow.Stop())
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"go one", "go two"}, utterances)
}

func TestAgentFlowContextCancellationAndInputErrors(t *testing.T) {
	input := &fakeAgentInput{}
	flow, err := NewAgentFlow(AgentFlowResources{Input: input}, AgentFlowConfig{})
	require.NoError(t, err)
	var seen error
	flow.OnError(func(err error) { seen = err })
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, flow.Start(ctx))
	inputErr := errors.New("input")
	input.emitError(inputErr)
	require.ErrorIs(t, seen, inputErr)
	cancel()
	require.Eventually(t, func() bool {
		flow.mu.RLock()
		defer flow.mu.RUnlock()
		return flow.run == nil
	}, time.Second, time.Millisecond)
}

func TestAgentFlowCloseHonorsOwnership(t *testing.T) {
	input := &fakeAgentInput{}
	speaker := &fakeAgentSpeaker{}
	output := &fakeAudioOutput{}
	embeddings := &closableEmbeddingBackend{fakeEmbeddingBackend: fakeEmbeddingBackend{}}
	flow, err := NewAgentFlow(AgentFlowResources{
		Input: input, Speaker: speaker, Output: output, Embeddings: embeddings,
	}, AgentFlowConfig{
		OwnInput: true, OwnSpeaker: true, OwnOutput: true, OwnEmbeddings: true,
	})
	require.NoError(t, err)
	require.NoError(t, flow.Close())
	require.NoError(t, flow.Close())
	assert.Equal(t, 1, input.closeCalls)
	assert.Equal(t, 1, speaker.closeCalls)
	assert.Equal(t, 1, output.closeCalls)
	assert.Equal(t, 1, embeddings.closeCalls)
	require.ErrorIs(t, flow.Register("x", []string{"x"}, func(context.Context, string) error { return nil }), ErrClosed)
}

type closableEmbeddingBackend struct {
	fakeEmbeddingBackend
	closeCalls int
}

func (c *closableEmbeddingBackend) Close() error { c.closeCalls++; return nil }

func TestAgentFlowRejectsInvalidConfigurationAndState(t *testing.T) {
	_, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{UseEmbeddings: true})
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{Threshold: 2})
	require.ErrorIs(t, err, ErrInvalidArgument)
	flow, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	require.ErrorIs(t, flow.Register("", nil, nil), ErrInvalidArgument)
	require.ErrorIs(t, flow.Start(context.Background()), ErrInvalidArgument)
	require.ErrorIs(t, flow.Say(context.Background(), "hello"), ErrInvalidArgument)
}
