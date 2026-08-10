package moonshine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	DefaultYesPhrases = []string{
		"yes", "yeah", "yep", "correct", "that's right", "sure", "affirmative", "okay", "please do", "do it",
	}
	DefaultNoPhrases = []string{
		"no", "nope", "incorrect", "that's wrong", "negative", "cancel", "don't do it", "stop",
	}
)

// AskOptions controls retry and timeout behavior for conversational prompts.
type AskOptions struct {
	Timeout    time.Duration
	Reprompt   string
	MaxRetries int
}

// DialogFlow is a straight-line conversational interaction. Prompt methods
// block until HandleUtterance supplies the next answer or the flow is cancelled.
type DialogFlow func(*Dialog) error

type dialogSession struct {
	ctx     context.Context
	cancel  context.CancelCauseFunc
	trigger string
	flow    DialogFlow
	answers chan string
	settled chan struct{}
	done    chan struct{}
}

func (s *dialogSession) signalSettled() {
	select {
	case s.settled <- struct{}{}:
	default:
	}
}

// Dialog is handed to a DialogFlow and provides blocking conversational
// operations while retaining its trigger phrase and caller-owned state.
type Dialog struct {
	agent   *AgentFlow
	session *dialogSession

	TriggerPhrase string
	State         map[string]any
}

// ListenFor registers a conversational flow for a trigger phrase.
func (a *AgentFlow) ListenFor(trigger string, flow DialogFlow) error {
	if a == nil {
		return ErrClosed
	}
	if strings.TrimSpace(trigger) == "" || flow == nil {
		return fmt.Errorf("invalid dialog flow: %w", ErrInvalidArgument)
	}
	a.mu.Lock()
	if a.closed || a.closing {
		a.mu.Unlock()
		return ErrClosed
	}
	a.dialogFlows[trigger] = flow
	a.mu.Unlock()
	return a.Register("dialog:"+trigger, []string{trigger}, func(ctx context.Context, _ string) error {
		return a.startDialog(ctx, trigger, flow)
	})
}

func (a *AgentFlow) startDialog(ctx context.Context, trigger string, flow DialogFlow) error {
	sessionCtx, cancel := context.WithCancelCause(ctx)
	session := &dialogSession{
		ctx: sessionCtx, cancel: cancel, trigger: trigger, flow: flow,
		answers: make(chan string), settled: make(chan struct{}, 1), done: make(chan struct{}),
	}
	a.mu.Lock()
	if a.closed || a.activeDialog != nil {
		a.mu.Unlock()
		cancel(ErrClosed)
		return ErrClosed
	}
	a.activeDialog = session
	a.mu.Unlock()

	go a.runDialog(session)
	select {
	case <-session.settled:
		return nil
	case <-ctx.Done():
		session.cancel(ctx.Err())
		<-session.done
		return ctx.Err()
	}
}

func (a *AgentFlow) runDialog(session *dialogSession) {
	var err error
	for {
		dialog := &Dialog{
			agent: a, session: session, TriggerPhrase: session.trigger, State: make(map[string]any),
		}
		err = session.flow(dialog)
		if !errors.Is(err, ErrDialogRestart) || context.Cause(session.ctx) != nil {
			break
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrDialogCancelled) && !errors.Is(err, ErrDialogRestart) {
		a.emitError(err)
	}
	a.mu.Lock()
	if a.activeDialog == session {
		a.activeDialog = nil
	}
	a.mu.Unlock()
	close(session.done)
	session.signalSettled()
}

func (a *AgentFlow) deliverDialogUtterance(ctx context.Context, utterance string) (bool, error) {
	a.mu.RLock()
	session := a.activeDialog
	a.mu.RUnlock()
	if session == nil {
		return false, nil
	}
	normalized := strings.ToLower(strings.TrimSpace(utterance))
	switch normalized {
	case "cancel":
		session.cancel(ErrDialogCancelled)
		<-session.done
		return true, nil
	case "start over":
		session.cancel(ErrDialogRestart)
		<-session.done
		return true, a.startDialog(ctx, session.trigger, session.flow)
	}
	select {
	case session.answers <- utterance:
	case <-session.done:
		return true, nil
	case <-ctx.Done():
		session.cancel(ctx.Err())
		<-session.done
		return true, ctx.Err()
	}
	select {
	case <-session.settled:
		return true, nil
	case <-ctx.Done():
		session.cancel(ctx.Err())
		<-session.done
		return true, ctx.Err()
	}
}

// IsActive reports whether a conversational flow is running or awaiting input.
func (a *AgentFlow) IsActive() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeDialog != nil
}

// ActiveTrigger returns the phrase that started the active flow.
func (a *AgentFlow) ActiveTrigger() (string, bool) {
	if a == nil {
		return "", false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.activeDialog == nil {
		return "", false
	}
	return a.activeDialog.trigger, true
}

// Cancel abandons the active flow. It returns false when no flow is active.
func (a *AgentFlow) Cancel() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	session := a.activeDialog
	a.mu.RUnlock()
	if session == nil {
		return false
	}
	session.cancel(ErrDialogCancelled)
	<-session.done
	return true
}

// Say speaks text and waits for playback to finish.
func (d *Dialog) Say(text string, options ...Option) error {
	if d == nil || d.session == nil {
		return ErrClosed
	}
	if err := context.Cause(d.session.ctx); err != nil {
		return err
	}
	if d.agent.resources.Speaker == nil && d.agent.resources.Output == nil {
		d.agent.emitSaid(text)
		return nil
	}
	return d.agent.Say(d.session.ctx, text, options...)
}

// Ask speaks a prompt and returns the next non-empty utterance.
func (d *Dialog) Ask(prompt string, options AskOptions) (string, error) {
	return dialogPrompt(d, prompt, options, func(answer string) (string, bool) {
		answer = strings.TrimSpace(answer)
		return answer, answer != ""
	})
}

// Confirm asks a yes/no question using configurable phrase groups.
func (d *Dialog) Confirm(prompt string, yesPhrases, noPhrases []string, options AskOptions) (bool, error) {
	if len(yesPhrases) == 0 {
		yesPhrases = DefaultYesPhrases
	}
	if len(noPhrases) == 0 {
		noPhrases = DefaultNoPhrases
	}
	if options.MaxRetries == 0 {
		options.MaxRetries = 1
	}
	answer, err := dialogPrompt(d, prompt, options, func(answer string) (bool, bool) {
		if matchesAny(answer, yesPhrases) {
			return true, true
		}
		if matchesAny(answer, noPhrases) {
			return false, true
		}
		return false, false
	})
	return answer, err
}

// Choose asks a question and returns the key whose key or phrase matched.
func (d *Dialog) Choose(prompt string, choices map[string][]string, options AskOptions) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("dialog choices are empty: %w", ErrInvalidArgument)
	}
	return dialogPrompt(d, prompt, options, func(answer string) (string, bool) {
		for key, phrases := range choices {
			if matchesAny(answer, append([]string{key}, phrases...)) {
				return key, true
			}
		}
		return "", false
	})
}

func dialogPrompt[T any](d *Dialog, prompt string, options AskOptions, interpret func(string) (T, bool)) (T, error) {
	var zero T
	if strings.TrimSpace(prompt) == "" || options.Timeout < 0 || options.MaxRetries < 0 {
		return zero, fmt.Errorf("invalid dialog prompt: %w", ErrInvalidArgument)
	}
	retries := options.MaxRetries
	if retries == 0 {
		retries = 2
	}
	for attempt := 0; ; attempt++ {
		spoken := prompt
		if attempt > 0 {
			spoken = options.Reprompt
			if spoken == "" {
				spoken = "Sorry, I didn't get that. " + prompt
			}
			spoken = strings.ReplaceAll(spoken, "{prompt}", prompt)
		}
		if err := d.Say(spoken); err != nil {
			return zero, err
		}
		d.session.signalSettled()
		answer, err := d.waitForAnswer(options.Timeout)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return zero, err
		}
		if err == nil {
			if value, ok := interpret(answer); ok {
				return value, nil
			}
		}
		if attempt >= retries {
			return zero, ErrDialogNoMatch
		}
	}
}

func (d *Dialog) waitForAnswer(timeout time.Duration) (string, error) {
	if timeout <= 0 {
		select {
		case answer := <-d.session.answers:
			return answer, nil
		case <-d.session.ctx.Done():
			return "", context.Cause(d.session.ctx)
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case answer := <-d.session.answers:
		return answer, nil
	case <-timer.C:
		return "", context.DeadlineExceeded
	case <-d.session.ctx.Done():
		return "", context.Cause(d.session.ctx)
	}
}

// Cancel aborts this dialog.
func (d *Dialog) Cancel() error { return ErrDialogCancelled }

// Restart asks the runner to start this flow again from its trigger.
func (d *Dialog) Restart() error { return ErrDialogRestart }

func matchesAny(utterance string, phrases []string) bool {
	normalized := strings.ToLower(strings.TrimSpace(utterance))
	for _, phrase := range phrases {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if phrase != "" && (normalized == phrase || strings.Contains(normalized, phrase)) {
			return true
		}
	}
	return false
}

// SpellOut separates a value into characters suitable for spoken feedback.
func SpellOut(value string) string { return strings.Join(strings.Split(value, ""), " ") }
