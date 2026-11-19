package model

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestPromptQueue_DuplicateTextMatchesOneToOne(t *testing.T) {
	t.Parallel()

	var queue promptQueue
	queue.SetFetched([]string{"continue"})
	queue.Add("continue")
	require.Equal(t, []string{"continue", "continue"}, queue.Items())

	// The fetch still reporting only the first one claims the pending
	// copy: the known, brief under-count.
	queue.SetFetched([]string{"continue"})
	require.Equal(t, 1, queue.Shown())

	queue.SetFetched([]string{"continue", "continue"})
	require.Equal(t, 2, queue.Shown())
}

func TestPromptQueue_SettledOutlivesMissingFetch(t *testing.T) {
	t.Parallel()

	var queue promptQueue
	settled := queue.Add("settled")
	queue.Settle(settled)
	queue.SetFetched(nil)
	require.Equal(t, []string{"settled"}, queue.Items(),
		"a fetch that lands before the server queues the prompt must not drop it")

	queue.Add("in flight")
	queue.DropSettled()
	require.Equal(t, []string{"in flight"}, queue.Items(),
		"an idle session drops only the prompts the server already accepted")
}

func TestPromptQueue_StartedTakesOldest(t *testing.T) {
	t.Parallel()

	var queue promptQueue
	queue.SetFetched([]string{"again"})
	queue.Add("again")
	queue.Started("again")
	queue.SetFetched(nil)
	require.Equal(t, []string{"again"}, queue.Items(),
		"the queued copy started, so the newer submission still shows")
}

// queueWorkspace is a countingWorkspace whose AgentRun enqueues the prompt
// behind the busy turn, or fails with err. With lagging set it returns
// without queueing, as the server answers before it has built the run.
type queueWorkspace struct {
	*countingWorkspace
	err     error
	lagging bool
}

func (w *queueWorkspace) AgentRun(_ context.Context, _ string, prompt string, _ ...message.Attachment) error {
	if w.err != nil {
		return w.err
	}
	if !w.lagging {
		w.queued = append(w.queued, prompt)
	}
	return nil
}

// newQueueUI returns a UI whose session s1 has a turn in flight.
func newQueueUI(t *testing.T) (*UI, *queueWorkspace) {
	t.Helper()
	pinTTLs(t)
	ws := &queueWorkspace{countingWorkspace: &countingWorkspace{ready: true, agentBusy: true}}
	m := newBusyUI(ws.countingWorkspace)
	m.com.Workspace = ws
	warmCaches(m, true)
	m.probedSession, m.probedSessionBusy = "s1", true
	return m, ws
}

// submit sends prompt and runs the submission, returning the
// agentRunSubmittedMsg without applying it.
func submit(t *testing.T, m *UI, prompt string) agentRunSubmittedMsg {
	t.Helper()
	batch, ok := m.sendMessage(prompt)().(tea.BatchMsg)
	require.True(t, ok)
	var submitted *agentRunSubmittedMsg
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(agentRunSubmittedMsg); ok {
			submitted = &msg
		}
	}
	require.NotNil(t, submitted)
	return *submitted
}

// fetch applies a queue fetch of the workspace's current queue.
func fetch(m *UI, ws *queueWorkspace) {
	m.applyPromptQueue(promptQueueMsg{forSession: "s1", gen: m.promptQueueGen, prompts: ws.queued})
}

func TestQueuedPrompt_ShowsOnSubmit(t *testing.T) {
	m, ws := newQueueUI(t)

	batch := m.sendMessage("next")
	require.Equal(t, []string{"next"}, m.promptQueue.Items(), "the pill shows before the agent enqueues")

	// The agent enqueues it; the confirming fetch shows it once.
	for _, cmd := range batch().(tea.BatchMsg) {
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(agentRunSubmittedMsg); ok {
			_, next := m.Update(msg)
			runCmds(m, next)
		}
	}
	require.Equal(t, []string{"next"}, ws.queued)
	require.Equal(t, []string{"next"}, m.promptQueue.Items())
}

func TestQueuedPrompt_RejectedSubmitLeavesPill(t *testing.T) {
	m, ws := newQueueUI(t)
	ws.err = errors.New("no")

	msg := submit(t, m, "next")
	require.Equal(t, 1, m.promptQueue.Shown())

	_, next := m.Update(msg)
	require.Zero(t, m.promptQueue.Shown(), "a rejected prompt leaves the pill at once")
	runCmds(m, next)
	require.Zero(t, m.promptQueue.Shown())
}

// TestQueuedPrompt_ServerLagKeepsPill covers client/server mode: the server
// accepts a prompt before it queues it, so the fetch that follows the
// submission can miss it.
func TestQueuedPrompt_ServerLagKeepsPill(t *testing.T) {
	t.Run("until its turn starts", func(t *testing.T) {
		m, ws := newQueueUI(t)
		ws.lagging = true

		_, next := m.Update(submit(t, m, "next"))
		runCmds(m, next)
		require.Equal(t, []string{"next"}, m.promptQueue.Items(),
			"a fetch that lands before the server queues the prompt must keep it")

		_, next = m.Update(pubsub.Event[message.Message]{
			Type: pubsub.CreatedEvent,
			Payload: message.Message{
				ID: "m1", SessionID: "s1", Role: message.User,
				Parts: []message.ContentPart{message.TextContent{Text: "next"}},
			},
		})
		require.Zero(t, m.promptQueue.Shown(), "a started prompt leaves the pill")
		runCmds(m, next)
		require.Zero(t, m.promptQueue.Shown())
	})

	t.Run("until the session is idle", func(t *testing.T) {
		m, ws := newQueueUI(t)
		ws.lagging = true

		_, next := m.Update(submit(t, m, "next"))
		runCmds(m, next)
		require.Equal(t, 1, m.promptQueue.Shown())

		ws.agentBusy = false
		m.invalidateBusyCaches()
		runCmds(m, m.dispatchBusyRefresh())
		require.Zero(t, m.promptQueue.Shown(), "an idle session holds no queued prompt")
	})

	t.Run("until a run on the session fails", func(t *testing.T) {
		m, ws := newQueueUI(t)
		ws.lagging = true

		_, next := m.Update(submit(t, m, "next"))
		runCmds(m, next)
		require.Equal(t, 1, m.promptQueue.Shown())

		agentError := func(sessionID string) tea.Cmd {
			_, cmd := m.Update(pubsub.Event[notify.Notification]{
				Type:    pubsub.CreatedEvent,
				Payload: notify.Notification{Type: notify.TypeAgentError, SessionID: sessionID},
			})
			return cmd
		}
		runCmds(m, agentError("s2"))
		require.Equal(t, 1, m.promptQueue.Shown(), "another session's failure leaves this queue alone")

		// The run failed before the server queued its prompt, and the
		// turn it waited behind still runs.
		next = agentError("s1")
		require.Zero(t, m.promptQueue.Shown(), "a failed run's prompt leaves the pill while the session is busy")
		runCmds(m, next)
		require.Zero(t, m.promptQueue.Shown())
	})
}

// TestQueuedPrompt_DequeuedBeforeSubmitSettles covers a fetch that sees the
// prompt, then the turn consuming it, all before AgentRun returns: the
// consumed prompt must not come back.
func TestQueuedPrompt_DequeuedBeforeSubmitSettles(t *testing.T) {
	m, ws := newQueueUI(t)

	msg := submit(t, m, "next")
	fetch(m, ws)
	require.Equal(t, []string{"next"}, m.promptQueue.Items())

	ws.queued = nil
	fetch(m, ws)
	require.Zero(t, m.promptQueue.Shown(), "a consumed prompt must not reappear")

	_, next := m.Update(msg)
	runCmds(m, next)
	require.Zero(t, m.promptQueue.Shown())
}

// TestQueuedPrompt_ReportTurnTakesItsOwnEntry covers a subagent report's
// turn: its message has no text, but its queue entry is the report's
// prompt text, so an attachment-only prompt (empty text) must stay.
func TestQueuedPrompt_ReportTurnTakesItsOwnEntry(t *testing.T) {
	m, _ := newQueueUI(t)
	report := message.SubAgentReport{Label: "akiko-bugs", Output: "all good"}
	m.promptQueue.SetFetched([]string{report.PromptText()})
	m.promptQueue.Settle(m.promptQueue.Add(""))

	m.Update(pubsub.Event[message.Message]{
		Type: pubsub.CreatedEvent,
		Payload: message.Message{
			ID: "m1", SessionID: "s1", Role: message.User,
			Parts: []message.ContentPart{report},
		},
	})
	require.Equal(t, []string{""}, m.promptQueue.Items(),
		"the report's turn takes the report off the pill, not the attachment-only prompt")
}

func TestQueuedPrompt_SessionSwitchDropsPending(t *testing.T) {
	m, ws := newQueueUI(t)

	submit(t, m, "next")
	ws.queued = nil
	_, cmd := m.Update(loadSessionMsg{session: &session.Session{ID: "s2"}})
	require.Zero(t, m.promptQueue.Shown())
	runCmds(m, cmd)
	require.Zero(t, m.promptQueue.Shown())
}

func TestQueuedPrompt_HiddenNeverShows(t *testing.T) {
	m, _ := newQueueUI(t)

	_ = m.sendMessageInternal("continue the plan", true)
	require.Zero(t, m.promptQueue.Shown(), "a hidden prompt stays out of the pill")
}
