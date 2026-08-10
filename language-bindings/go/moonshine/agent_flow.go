package moonshine

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type AgentInput interface {
	Start(context.Context) error
	Stop() error
	AddListener(func(TranscriptEvent)) func()
	AddErrorListener(func(error)) func()
	Close() error
}

type AgentSpeaker interface {
	Synthesize(text string, options ...Option) (Audio, error)
}

type AudioOutput interface {
	Play(context.Context, Audio) error
	Close() error
}

type AgentFlowConfig struct {
	Threshold     float32
	UseEmbeddings bool
	OwnInput      bool
	OwnSpeaker    bool
	OwnOutput     bool
	OwnEmbeddings bool
}

type AgentFlowResources struct {
	Input      AgentInput
	Speaker    AgentSpeaker
	Output     AudioOutput
	Embeddings EmbeddingBackend
}

type AgentHandler func(context.Context, string) error

type utteranceMatcher interface {
	MatchUtterance(string) (MatchResult, error)
}

type agentRun struct {
	ctx       context.Context
	cancel    context.CancelFunc
	wake      chan struct{}
	done      chan struct{}
	mu        sync.Mutex
	pending   []string
	accepting bool
	stopping  bool
	stopOnce  sync.Once
	err       error
}

// AgentFlow routes completed utterances to registered handlers and composes
// microphone input, semantic matching, TTS, and audio playback behind narrow
// injectable interfaces.
type AgentFlow struct {
	resources AgentFlowResources
	config    AgentFlowConfig

	mu             sync.RWMutex
	phrases        map[string][]string
	handlers       map[string]AgentHandler
	otherwise      AgentHandler
	heardHandlers  map[uint64]func(string)
	saidHandlers   map[uint64]func(string)
	errorHandlers  map[uint64]func(error)
	nextListener   uint64
	generation     uint64
	matcher        utteranceMatcher
	matcherGen     uint64
	run            *agentRun
	removeInput    func()
	removeInputErr func()
	closed         bool
	closing        bool
	closeDone      chan struct{}
	closeErr       error
	matcherMu      sync.Mutex
	dialogFlows    map[string]DialogFlow
	activeDialog   *dialogSession
	globalHandlers map[string]AgentHandler
	globalOrder    []string
}

func NewAgentFlow(resources AgentFlowResources, config AgentFlowConfig) (*AgentFlow, error) {
	if config.Threshold == 0 {
		config.Threshold = 0.55
	}
	if config.Threshold < -1 || config.Threshold > 1 {
		return nil, fmt.Errorf("invalid AgentFlow threshold: %w", ErrInvalidArgument)
	}
	if config.UseEmbeddings && resources.Embeddings == nil {
		return nil, fmt.Errorf("AgentFlow embeddings are required: %w", ErrInvalidArgument)
	}
	return &AgentFlow{
		resources:      resources,
		config:         config,
		phrases:        make(map[string][]string),
		handlers:       make(map[string]AgentHandler),
		heardHandlers:  make(map[uint64]func(string)),
		saidHandlers:   make(map[uint64]func(string)),
		errorHandlers:  make(map[uint64]func(error)),
		dialogFlows:    make(map[string]DialogFlow),
		globalHandlers: make(map[string]AgentHandler),
		closeDone:      make(chan struct{}),
	}, nil
}

func (a *AgentFlow) Register(key string, phrases []string, handler AgentHandler) error {
	if a == nil {
		return ErrClosed
	}
	if key == "" || len(phrases) == 0 || handler == nil {
		return fmt.Errorf("invalid AgentFlow registration: %w", ErrInvalidArgument)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.closing {
		return ErrClosed
	}
	a.phrases[key] = append([]string(nil), phrases...)
	a.handlers[key] = handler
	a.generation++
	return nil
}

func (a *AgentFlow) Unregister(key string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, found := a.handlers[key]; !found {
		return false
	}
	delete(a.handlers, key)
	delete(a.phrases, key)
	a.generation++
	return true
}

func (a *AgentFlow) Otherwise(handler AgentHandler) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.otherwise = handler
	a.mu.Unlock()
}

func (a *AgentFlow) matcherForCurrentGeneration() (utteranceMatcher, error) {
	a.matcherMu.Lock()
	defer a.matcherMu.Unlock()
	a.mu.RLock()
	if a.closed {
		a.mu.RUnlock()
		return nil, ErrClosed
	}
	if a.matcher != nil && a.matcherGen == a.generation {
		matcher := a.matcher
		a.mu.RUnlock()
		return matcher, nil
	}
	generation := a.generation
	phrases := make(map[string][]string, len(a.phrases))
	for key, values := range a.phrases {
		phrases[key] = append([]string(nil), values...)
	}
	a.mu.RUnlock()
	var matcher utteranceMatcher
	var err error
	if a.config.UseEmbeddings {
		matcher, err = NewPhraseMatcher(a.resources.Embeddings, phrases, a.config.Threshold)
	} else {
		matcher, err = NewSubstringMatcher(phrases, a.config.Threshold)
	}
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.generation == generation && !a.closed {
		a.matcher = matcher
		a.matcherGen = generation
	}
	a.mu.Unlock()
	return matcher, nil
}

func (a *AgentFlow) HandleUtterance(ctx context.Context, utterance string) (bool, error) {
	if a == nil {
		return false, ErrClosed
	}
	if ctx == nil {
		return false, fmt.Errorf("nil AgentFlow context: %w", ErrInvalidArgument)
	}
	a.emitHeard(utterance)
	if claimed, err := a.handleGlobal(ctx, utterance); claimed || err != nil {
		return claimed, err
	}
	if claimed, err := a.deliverDialogUtterance(ctx, utterance); claimed || err != nil {
		return claimed, err
	}
	matcher, err := a.matcherForCurrentGeneration()
	if err != nil {
		a.emitError(err)
		return false, err
	}
	result, err := matcher.MatchUtterance(utterance)
	if err != nil {
		a.emitError(err)
		return false, err
	}
	a.mu.RLock()
	handler := a.otherwise
	if result.Found {
		handler = a.handlers[result.Key]
	}
	a.mu.RUnlock()
	if handler == nil {
		return result.Found, nil
	}
	if err := handler(ctx, utterance); err != nil {
		a.emitError(err)
		return result.Found, err
	}
	return result.Found, nil
}

// Say synthesizes and plays text through the configured output.
func (a *AgentFlow) Say(ctx context.Context, text string, options ...Option) error {
	if a == nil {
		return ErrClosed
	}
	if a.resources.Speaker == nil || a.resources.Output == nil {
		return fmt.Errorf("AgentFlow speech is not configured: %w", ErrInvalidArgument)
	}
	audio, err := a.resources.Speaker.Synthesize(text, options...)
	if err == nil {
		err = a.resources.Output.Play(ctx, audio)
	}
	if err != nil {
		a.emitError(err)
		return err
	}
	a.emitSaid(text)
	return nil
}

func (a *AgentFlow) Start(ctx context.Context) error {
	if a == nil {
		return ErrClosed
	}
	if ctx == nil {
		return fmt.Errorf("nil AgentFlow context: %w", ErrInvalidArgument)
	}
	a.mu.Lock()
	if a.closed || a.closing {
		a.mu.Unlock()
		return ErrClosed
	}
	if a.run != nil {
		a.mu.Unlock()
		return nil
	}
	if a.resources.Input == nil {
		a.mu.Unlock()
		return fmt.Errorf("AgentFlow input is not configured: %w", ErrInvalidArgument)
	}
	runCtx, cancel := context.WithCancel(ctx)
	run := &agentRun{
		ctx: runCtx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), accepting: true,
	}
	a.run = run
	a.removeInput = a.resources.Input.AddListener(func(event TranscriptEvent) {
		if completed, ok := event.(LineCompleted); ok {
			a.enqueue(run, completed.Line.Text)
		}
	})
	a.removeInputErr = a.resources.Input.AddErrorListener(a.emitError)
	a.mu.Unlock()
	go a.work(run)
	if err := a.resources.Input.Start(runCtx); err != nil {
		_ = a.stopRun(run)
		return err
	}
	go func() {
		select {
		case <-runCtx.Done():
			_ = a.stopRun(run)
		case <-run.done:
		}
	}()
	return nil
}

func (a *AgentFlow) enqueue(run *agentRun, utterance string) {
	run.mu.Lock()
	if run.accepting {
		run.pending = append(run.pending, utterance)
	}
	run.mu.Unlock()
	select {
	case run.wake <- struct{}{}:
	default:
	}
}

func (a *AgentFlow) work(run *agentRun) {
	defer close(run.done)
	for {
		<-run.wake
		for {
			run.mu.Lock()
			if len(run.pending) == 0 {
				stopping := run.stopping
				run.mu.Unlock()
				if stopping {
					return
				}
				break
			}
			utterance := run.pending[0]
			run.pending = run.pending[1:]
			run.mu.Unlock()
			_, _ = a.HandleUtterance(run.ctx, utterance)
		}
	}
}

func (a *AgentFlow) Stop() error {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	run := a.run
	a.mu.RUnlock()
	if run == nil {
		return nil
	}
	return a.stopRun(run)
}

func (a *AgentFlow) stopRun(run *agentRun) error {
	run.stopOnce.Do(func() {
		run.err = a.resources.Input.Stop()
		run.mu.Lock()
		run.accepting = false
		run.stopping = true
		run.mu.Unlock()
		run.cancel()
		select {
		case run.wake <- struct{}{}:
		default:
		}
		<-run.done
		a.mu.Lock()
		if a.run == run {
			a.run = nil
			if a.removeInput != nil {
				a.removeInput()
			}
			if a.removeInputErr != nil {
				a.removeInputErr()
			}
			a.removeInput = nil
			a.removeInputErr = nil
		}
		a.mu.Unlock()
		if run.err != nil {
			a.emitError(run.err)
		}
	})
	return run.err
}

func (a *AgentFlow) OnHeard(listener func(string)) func() {
	return a.addStringListener(a.heardHandlers, listener)
}

func (a *AgentFlow) OnSaid(listener func(string)) func() {
	return a.addStringListener(a.saidHandlers, listener)
}

func (a *AgentFlow) addStringListener(target map[uint64]func(string), listener func(string)) func() {
	if a == nil || listener == nil {
		return func() {}
	}
	a.mu.Lock()
	id := a.nextListener
	a.nextListener++
	target[id] = listener
	a.mu.Unlock()
	return a.removeListener(func() { delete(target, id) })
}

func (a *AgentFlow) OnError(listener func(error)) func() {
	if a == nil || listener == nil {
		return func() {}
	}
	a.mu.Lock()
	id := a.nextListener
	a.nextListener++
	a.errorHandlers[id] = listener
	a.mu.Unlock()
	return a.removeListener(func() { delete(a.errorHandlers, id) })
}

func (a *AgentFlow) removeListener(remove func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			remove()
			a.mu.Unlock()
		})
	}
}

func (a *AgentFlow) emitHeard(text string) { a.emitStrings(a.heardHandlers, text) }
func (a *AgentFlow) emitSaid(text string)  { a.emitStrings(a.saidHandlers, text) }

func (a *AgentFlow) emitStrings(source map[uint64]func(string), text string) {
	a.mu.RLock()
	listeners := make([]func(string), 0, len(source))
	for _, listener := range source {
		listeners = append(listeners, listener)
	}
	a.mu.RUnlock()
	for _, listener := range listeners {
		listener(text)
	}
}

func (a *AgentFlow) emitError(err error) {
	if err == nil {
		return
	}
	a.mu.RLock()
	listeners := make([]func(error), 0, len(a.errorHandlers))
	for _, listener := range a.errorHandlers {
		listeners = append(listeners, listener)
	}
	a.mu.RUnlock()
	for _, listener := range listeners {
		listener(err)
	}
}

func (a *AgentFlow) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return a.closeErr
	}
	if a.closing {
		done := a.closeDone
		a.mu.Unlock()
		<-done
		a.mu.RLock()
		defer a.mu.RUnlock()
		return a.closeErr
	}
	a.closing = true
	a.mu.Unlock()
	a.Cancel()
	err := a.Stop()
	if a.config.OwnInput && a.resources.Input != nil {
		err = errors.Join(err, a.resources.Input.Close())
	}
	if a.config.OwnSpeaker {
		if closer, ok := a.resources.Speaker.(interface{ Close() error }); ok {
			err = errors.Join(err, closer.Close())
		}
	}
	if a.config.OwnOutput && a.resources.Output != nil {
		err = errors.Join(err, a.resources.Output.Close())
	}
	if a.config.OwnEmbeddings {
		if closer, ok := a.resources.Embeddings.(interface{ Close() error }); ok {
			err = errors.Join(err, closer.Close())
		}
	}
	a.mu.Lock()
	a.closed = true
	a.closing = false
	a.closeErr = err
	close(a.closeDone)
	a.mu.Unlock()
	return err
}
