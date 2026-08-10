package moonshine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialogRunsAskAndConfirmToCompletion(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	var spoken []string
	agent.OnSaid(func(text string) { spoken = append(spoken, text) })
	var network string
	var confirmed bool
	require.NoError(t, agent.ListenFor("set up wifi", func(dialog *Dialog) error {
		var err error
		network, err = dialog.Ask("What's the network?", AskOptions{})
		if err != nil {
			return err
		}
		confirmed, err = dialog.Confirm("Use "+network+"?", nil, nil, AskOptions{})
		if err != nil {
			return err
		}
		return dialog.Say("Done.")
	}))

	claimed, err := agent.HandleUtterance(context.Background(), "please set up wifi")
	require.NoError(t, err)
	assert.True(t, claimed)
	assert.True(t, agent.IsActive())
	trigger, ok := agent.ActiveTrigger()
	assert.True(t, ok)
	assert.Equal(t, "set up wifi", trigger)
	claimed, err = agent.HandleUtterance(context.Background(), "home network")
	require.NoError(t, err)
	assert.True(t, claimed)
	claimed, err = agent.HandleUtterance(context.Background(), "yes")
	require.NoError(t, err)
	assert.True(t, claimed)

	assert.Equal(t, "home network", network)
	assert.True(t, confirmed)
	assert.Equal(t, []string{"What's the network?", "Use home network?", "Done."}, spoken)
	assert.False(t, agent.IsActive())
}

func TestDialogConfirmUnderstandsNoAndChooseReturnsKey(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	var confirmed bool
	var choice string
	require.NoError(t, agent.ListenFor("pick", func(dialog *Dialog) error {
		var err error
		confirmed, err = dialog.Confirm("Ready?", nil, nil, AskOptions{})
		if err != nil {
			return err
		}
		choice, err = dialog.Choose("Which band?", map[string][]string{
			"2.4 GHz": {"slow", "long range"},
			"5 GHz":   {"fast"},
		}, AskOptions{})
		return err
	}))

	_, err = agent.HandleUtterance(context.Background(), "pick")
	require.NoError(t, err)
	_, err = agent.HandleUtterance(context.Background(), "nope")
	require.NoError(t, err)
	_, err = agent.HandleUtterance(context.Background(), "the fast one")
	require.NoError(t, err)

	assert.False(t, confirmed)
	assert.Equal(t, "5 GHz", choice)
}

func TestDialogRepromptsThenReportsNoMatch(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	var spoken []string
	var seen error
	agent.OnSaid(func(text string) { spoken = append(spoken, text) })
	agent.OnError(func(err error) { seen = err })
	require.NoError(t, agent.ListenFor("begin", func(dialog *Dialog) error {
		_, err := dialog.Confirm("Ready?", nil, nil, AskOptions{MaxRetries: 1, Reprompt: "Yes or no: {prompt}"})
		return err
	}))

	_, err = agent.HandleUtterance(context.Background(), "begin")
	require.NoError(t, err)
	_, err = agent.HandleUtterance(context.Background(), "bananas")
	require.NoError(t, err)
	_, err = agent.HandleUtterance(context.Background(), "more bananas")
	require.NoError(t, err)

	require.ErrorIs(t, seen, ErrDialogNoMatch)
	assert.Equal(t, []string{"Ready?", "Yes or no: Ready?"}, spoken)
	assert.False(t, agent.IsActive())
}

func TestDialogBuiltInCancelAndRestart(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	starts := 0
	finished := 0
	require.NoError(t, agent.ListenFor("begin", func(dialog *Dialog) error {
		starts++
		_, err := dialog.Ask("Name?", AskOptions{})
		if err == nil {
			finished++
		}
		return err
	}))

	_, err = agent.HandleUtterance(context.Background(), "begin")
	require.NoError(t, err)
	_, err = agent.HandleUtterance(context.Background(), "start over")
	require.NoError(t, err)
	assert.Equal(t, 2, starts)
	assert.True(t, agent.IsActive())
	_, err = agent.HandleUtterance(context.Background(), "cancel")
	require.NoError(t, err)
	assert.Equal(t, 0, finished)
	assert.False(t, agent.IsActive())
	assert.False(t, agent.Cancel())
}

func TestDialogMethodsReturnSentinels(t *testing.T) {
	assert.ErrorIs(t, (&Dialog{}).Cancel(), ErrDialogCancelled)
	assert.ErrorIs(t, (&Dialog{}).Restart(), ErrDialogRestart)
	assert.Equal(t, "w i f i", SpellOut("wifi"))
	assert.False(t, errors.Is(ErrDialogCancelled, ErrDialogRestart))
}

func TestAgentFlowStopCancelsDialogWaitingOnInput(t *testing.T) {
	input := &fakeAgentInput{}
	agent, err := NewAgentFlow(AgentFlowResources{Input: input}, AgentFlowConfig{})
	require.NoError(t, err)
	require.NoError(t, agent.ListenFor("begin", func(dialog *Dialog) error {
		_, err := dialog.Ask("Name?", AskOptions{})
		return err
	}))
	require.NoError(t, agent.Start(context.Background()))
	input.emit(LineCompleted{Line: TranscriptLine{Text: "begin"}})
	require.Eventually(t, agent.IsActive, time.Second, time.Millisecond)

	require.NoError(t, agent.Stop())

	assert.False(t, agent.IsActive())
}

func TestDialogBuiltInsDoNotClaimSpeechOutsideAFlow(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	var leftovers []string
	agent.Otherwise(func(_ context.Context, utterance string) error {
		leftovers = append(leftovers, utterance)
		return nil
	})

	for _, utterance := range []string{"cancel", "start over", "cancel my subscription tomorrow"} {
		claimed, err := agent.HandleUtterance(context.Background(), utterance)
		require.NoError(t, err)
		assert.False(t, claimed)
	}

	assert.Equal(t, []string{"cancel", "start over", "cancel my subscription tomorrow"}, leftovers)
}

func TestAgentFlowAlwaysClaimsBuiltInPhraseEverywhere(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	var calls []string
	require.NoError(t, agent.Always("cancel", func(_ context.Context, utterance string) error {
		calls = append(calls, utterance)
		return nil
	}))

	claimed, err := agent.HandleUtterance(context.Background(), "cancel")
	require.NoError(t, err)
	assert.True(t, claimed)
	require.NoError(t, agent.ListenFor("begin", func(dialog *Dialog) error {
		_, err := dialog.Ask("Name?", AskOptions{})
		return err
	}))
	_, err = agent.HandleUtterance(context.Background(), "begin")
	require.NoError(t, err)
	claimed, err = agent.HandleUtterance(context.Background(), "cancel")
	require.NoError(t, err)
	assert.True(t, claimed)
	assert.True(t, agent.IsActive())
	assert.Equal(t, []string{"cancel", "cancel"}, calls)
	assert.True(t, agent.UnregisterAlways("cancel"))
	assert.False(t, agent.UnregisterAlways("cancel"))
	assert.True(t, agent.Cancel())
}

func TestAgentFlowAlwaysReportsHandlerErrors(t *testing.T) {
	want := errors.New("global failed")
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	var seen error
	agent.OnError(func(err error) { seen = err })
	require.NoError(t, agent.Always("break", func(context.Context, string) error { return want }))

	claimed, err := agent.HandleUtterance(context.Background(), "break")

	assert.True(t, claimed)
	require.ErrorIs(t, err, want)
	require.ErrorIs(t, seen, want)
}

func TestAgentFlowListsAndUnregistersDialogFlows(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	flow := func(*Dialog) error { return nil }
	require.NoError(t, agent.ListenFor("first", flow))
	require.NoError(t, agent.ListenFor("second", flow))
	require.NoError(t, agent.ListenFor("first", flow))
	assert.Equal(t, []string{"first", "second"}, agent.RegisteredFlows())

	assert.True(t, agent.UnregisterFlow("first"))
	assert.False(t, agent.UnregisterFlow("first"))
	assert.Equal(t, []string{"second"}, agent.RegisteredFlows())
	claimed, err := agent.HandleUtterance(context.Background(), "first")
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestDialogAskTimesOutRepromptsAndStops(t *testing.T) {
	agent, err := NewAgentFlow(AgentFlowResources{}, AgentFlowConfig{})
	require.NoError(t, err)
	seen := make(chan error, 1)
	agent.OnError(func(err error) { seen <- err })
	require.NoError(t, agent.ListenFor("begin", func(dialog *Dialog) error {
		_, err := dialog.Ask("Name?", AskOptions{Timeout: time.Millisecond, MaxRetries: 1})
		return err
	}))

	_, err = agent.HandleUtterance(context.Background(), "begin")
	require.NoError(t, err)
	select {
	case err := <-seen:
		require.ErrorIs(t, err, ErrDialogNoMatch)
	case <-time.After(time.Second):
		t.Fatal("dialog did not report timeout")
	}
	assert.False(t, agent.IsActive())
}
