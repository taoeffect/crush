package agent

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// killedTurn seeds the shape a process killed mid-tool-call leaves behind: an
// assistant message holding a tool call, no tool result anywhere, and no
// finish part. finished says whether the call's arguments had finished
// arriving before the process died.
func killedTurn(t *testing.T, env fakeEnv, finished bool) (session.Session, message.Message) {
	t.Helper()
	ctx := t.Context()

	sess, err := env.sessions.Create(ctx, "killed")
	require.NoError(t, err)

	_, err = env.messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "run the thing"}},
	})
	require.NoError(t, err)

	input := `{"command":"sleep 30"}`
	if !finished {
		input = ""
	}
	assistant, err := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ToolCall{
				ID:       "call_killed",
				Name:     "bash",
				Input:    input,
				Finished: finished,
			},
		},
	})
	require.NoError(t, err)
	return sess, assistant
}

// A reader of the transcript settles orphans too, not just the next run.
// Without this a session opened after a crash shows the call as still in
// flight, and the first message of the next run rewrites the transcript
// under the user.
func TestListSettledMessages_SettlesBeforeAnyRun(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()

	sess, assistant := killedTurn(t, env, false)

	listSettled(t, env, sess.ID)

	stored, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	var result *message.ToolResult
	for _, msg := range stored {
		for _, tr := range msg.ToolResults() {
			if tr.ToolCallID == "call_killed" {
				tr := tr
				result = &tr
			}
		}
	}
	require.NotNil(t, result, "opening the session must settle the orphan")
	require.True(t, result.IsError)

	reloaded, err := env.messages.Get(ctx, assistant.ID)
	require.NoError(t, err)
	call := reloaded.ToolCalls()[0]
	require.True(t, call.Finished, "a call that never got its arguments is settled")
	require.Equal(t, "{}", call.Input, "the settled call carries valid JSON arguments")
	require.True(t, reloaded.IsFinished(), "the turn is marked finished")
	require.Equal(t, message.FinishReasonError, reloaded.FinishReason())
}

// The finish part records when the turn actually stopped. Stamping it with
// now would tell the response-time statistics that a turn killed months ago
// took months to answer.
func TestListSettledMessages_BackdatesTheFinish(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()

	sess, assistant := killedTurn(t, env, true)

	stale := staleMessages{Service: env.messages}
	before, err := stale.List(ctx, sess.ID)
	require.NoError(t, err)
	var stoppedAt int64
	for _, msg := range before {
		if msg.ID == assistant.ID {
			stoppedAt = msg.UpdatedAt
		}
	}
	require.NotZero(t, stoppedAt)

	agent := NewSessionAgent(SessionAgentOptions{Models: noModels, Sessions: env.sessions, Messages: stale})
	_, err = agent.ListSettledMessages(ctx, sess.ID)
	require.NoError(t, err)

	reloaded, err := env.messages.Get(ctx, assistant.ID)
	require.NoError(t, err)
	finish := reloaded.FinishPart()
	require.NotNil(t, finish)
	require.Equal(t, stoppedAt, finish.Time, "the finish is stamped with when the turn stopped")
}

// staleMessages reports every message as last written an hour before it
// was, like a session reopened long after its run died.
type staleMessages struct {
	message.Service
}

func (s staleMessages) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	msgs, err := s.Service.List(ctx, sessionID)
	for i := range msgs {
		msgs[i].UpdatedAt -= 3600
	}
	return msgs, err
}

// A turn can end cleanly and still lose a tool result, so a present finish
// part must not be taken as proof that nothing is dangling.
func TestListSettledMessages_RepairsOrphanOnFinishedTurn(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()

	sess, err := env.sessions.Create(ctx, "finished-but-orphaned")
	require.NoError(t, err)

	assistant, err := env.messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ToolCall{ID: "call_a", Name: "bash", Input: "{}", Finished: true},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 1000},
		},
	})
	require.NoError(t, err)

	listSettled(t, env, sess.ID)

	stored, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	var found bool
	for _, msg := range stored {
		for _, tr := range msg.ToolResults() {
			found = found || tr.ToolCallID == "call_a"
		}
	}
	require.True(t, found, "an orphan on a finished turn is still repaired")

	reloaded, err := env.messages.Get(ctx, assistant.ID)
	require.NoError(t, err)
	require.Equal(t, message.FinishReasonEndTurn, reloaded.FinishReason(), "an existing finish is left alone")
	require.Equal(t, int64(1000), reloaded.FinishPart().Time, "an existing finish is left alone")
}

// Parallel tool calls die together, and each needs its own result or the
// provider rejects the whole message.
func TestListSettledMessages_AnswersEachParallelCall(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()

	sess, err := env.sessions.Create(ctx, "parallel")
	require.NoError(t, err)

	_, err = env.messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ToolCall{ID: "call_1", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
			message.ToolCall{ID: "call_2", Name: "view", Input: "", Finished: false},
			message.ToolCall{ID: "call_3", Name: "grep", Input: `{"pattern":"x"}`, Finished: true},
		},
	})
	require.NoError(t, err)
	// Only the middle call got an answer before the process died.
	_, err = env.messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call_2", Name: "view", Content: "file"}},
	})
	require.NoError(t, err)

	listSettled(t, env, sess.ID)

	stored, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	results := map[string][]message.ToolResult{}
	for _, msg := range stored {
		for _, tr := range msg.ToolResults() {
			results[tr.ToolCallID] = append(results[tr.ToolCallID], tr)
		}
	}
	require.Len(t, results, 3, "every call ends up with a result")
	for id, rs := range results {
		require.Len(t, rs, 1, "call %s has exactly one result", id)
	}
	require.Equal(t, "file", results["call_2"][0].Content, "the real result is left alone")
	require.True(t, results["call_1"][0].IsError)
	require.True(t, results["call_3"][0].IsError)
}

func TestListSettledMessages_HealthySessionIsUntouched(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()

	sess, err := env.sessions.Create(ctx, "healthy")
	require.NoError(t, err)
	_, err = env.messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ToolCall{ID: "call_ok", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 1000},
		},
	})
	require.NoError(t, err)
	_, err = env.messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: []message.ContentPart{message.ToolResult{ToolCallID: "call_ok", Name: "bash", Content: "ok"}},
	})
	require.NoError(t, err)

	before, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	listSettled(t, env, sess.ID)
	after, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, after, len(before), "settling a healthy session writes nothing")
}

// listSettled reads a session through the agent, which settles orphans first.
func listSettled(t *testing.T, env fakeEnv, sessionID string) []message.Message {
	t.Helper()
	agent := NewSessionAgent(SessionAgentOptions{Models: noModels, Sessions: env.sessions, Messages: env.messages})
	msgs, err := agent.ListSettledMessages(t.Context(), sessionID)
	require.NoError(t, err)
	return msgs
}

// The returned transcript is the stored one, so callers need no second read.
func TestListSettledMessages_ReturnsWhatWasStored(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()

	sess, _ := killedTurn(t, env, true)

	got := listSettled(t, env, sess.ID)
	stored, err := env.messages.List(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, got, len(stored))
	for i := range stored {
		require.Equal(t, stored[i].ID, got[i].ID)
		require.Equal(t, stored[i].Parts, got[i].Parts)
	}
}

// A live run's unanswered call is not an orphan yet, so it is left alone.
func TestListSettledMessages_SkipsBusySessions(t *testing.T) {
	env := testEnv(t)

	sess, _ := killedTurn(t, env, true)

	agent := NewSessionAgent(SessionAgentOptions{Models: noModels, Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)
	agent.activeRequests.Set(sess.ID, &activeCancel{cancel: func() {}})

	msgs, err := agent.ListSettledMessages(t.Context(), sess.ID)
	require.NoError(t, err)
	for _, m := range msgs {
		require.Empty(t, m.ToolResults(), "a live run's unanswered call must be left alone")
	}
}
