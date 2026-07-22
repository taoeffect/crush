package tools

import (
	"context"
	"errors"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestNewSessionTool(t *testing.T) {
	t.Parallel()

	tool := NewNewSessionTool(true, true)
	info := tool.Info()

	require.Equal(t, NewSessionToolName, info.Name)
	require.NotEmpty(t, info.Description)
	require.Contains(t, info.Parameters, "summary")
}

func TestNewSessionToolDescriptionContextStatusEnabled(t *testing.T) {
	t.Parallel()

	desc := NewNewSessionTool(true, true).Info().Description

	require.Contains(t, desc, "<context_status>")
	require.Contains(t, desc, "used_pct")
	require.NotContains(t, desc, "No automatic context-usage indicator")
}

func TestNewSessionToolDescriptionContextStatusDisabled(t *testing.T) {
	t.Parallel()

	desc := NewNewSessionTool(false, true).Info().Description

	require.Contains(t, desc, "Invoke `new_session` only when the user instructs you to.")
	require.Contains(t, desc, "instructs you to.\n- The user may also override")
	require.NotContains(t, desc, "when you judge the conversation has grown long enough")
	require.NotContains(t, desc, "used_pct")
	require.NotContains(t, desc, "<context_status>")
}

func TestNewSessionToolDescriptionAutoSummarizeDisabled(t *testing.T) {
	t.Parallel()

	desc := NewNewSessionTool(false, false).Info().Description

	require.Contains(t, desc, "Invoke `new_session` when the user asks for one, or when you judge the conversation has grown long enough")
	require.Contains(t, desc, "lost context.\n- The user may also override")
	require.NotContains(t, desc, "only when the user instructs you to")
	require.NotContains(t, desc, "used_pct")
	require.NotContains(t, desc, "<context_status>")
}

func TestNewSessionToolReturnsError(t *testing.T) {
	t.Parallel()

	tool := NewNewSessionTool(true, true)
	summary := "Completed steps 1-3. Remaining: step 4 - write tests."

	_, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_123",
		Name:  NewSessionToolName,
		Input: `{"summary":"` + summary + `"}`,
	})

	require.Error(t, err)

	var nse *NewSessionError
	require.True(t, errors.As(err, &nse))
	require.Equal(t, summary, nse.Summary)
}

func TestNewSessionToolEmptySummary(t *testing.T) {
	t.Parallel()

	tool := NewNewSessionTool(true, true)

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_456",
		Name:  NewSessionToolName,
		Input: `{"summary":""}`,
	})

	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "non-empty summary")
}

func TestNewSessionToolWhitespaceSummary(t *testing.T) {
	t.Parallel()

	tool := NewNewSessionTool(false, true)

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_789",
		Name:  NewSessionToolName,
		Input: `{"summary":"   "}`,
	})

	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "non-empty summary")
}

func TestNewSessionErrorMessage(t *testing.T) {
	t.Parallel()

	err := &NewSessionError{Summary: "test"}
	require.Equal(t, "new session requested", err.Error())
}
