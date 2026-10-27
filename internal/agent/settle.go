package agent

import (
	"context"
	"errors"
	"log/slog"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/message"
)

// interruptedToolCallResult is the reply sent to the provider for a tool
// call that has no stored result.
const interruptedToolCallResult = "tool call was interrupted and did not produce a result, you may retry this call if the result is still needed"

// toolResultsForCalls builds the tool message that must immediately follow
// an assistant message with tool calls. Results are taken from
// toolResultsByCall and consumed, so a result stored alongside results for
// other assistants is emitted once, next to the call that asked for it. A
// call with no stored result gets a synthetic one; the in-turn finalize and
// RepairInterruptedSession should have made that impossible, so it is
// logged as a warning.
func toolResultsForCalls(m message.Message, toolResultsByCall map[string][]fantasy.MessagePart) fantasy.Message {
	content := make([]fantasy.MessagePart, 0, len(m.ToolCalls()))
	for _, tc := range m.ToolCalls() {
		parts := toolResultsByCall[tc.ID]
		delete(toolResultsByCall, tc.ID)
		if len(parts) > 0 {
			content = append(content, parts...)
			continue
		}
		slog.Warn(
			"Injecting synthetic tool result for orphaned tool call",
			"tool_call_id", tc.ID,
			"tool_name", tc.Name,
		)
		content = append(content, fantasy.ToolResultPart{
			ToolCallID: tc.ID,
			Output: fantasy.ToolResultOutputContentError{
				Error: errors.New(interruptedToolCallResult),
			},
		})
	}
	return fantasy.Message{
		Role:    fantasy.MessageRoleTool,
		Content: content,
	}
}

// ListSettledMessages returns a session's transcript after
// RepairInterruptedSession has given a terminal state to any turn a dead
// run left behind. It holds the session's dispatch lock throughout, so a
// run cannot start between the busy check and the repair. IsSessionBusy
// only reads activeRequests and takes no lock, so it is safe to call here.
func (a *sessionAgent) ListSettledMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	mu := a.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()

	if err := a.messages.FlushAll(ctx); err != nil {
		return nil, err
	}
	if !a.IsSessionBusy(sessionID) {
		if err := RepairInterruptedSession(ctx, a.messages, sessionID); err != nil {
			// The repair may have stopped halfway; return what is stored.
			slog.Warn("Failed to repair interrupted session", "session_id", sessionID, "error", err)
		}
	}
	return a.messages.List(ctx, sessionID)
}
