package tools

import (
	"bytes"
	"context"
	_ "embed"
	"html/template"
	"strings"

	"charm.land/fantasy"
)

type NewSessionParams struct {
	Summary string `json:"summary" description:"A summary of what has been accomplished so far and what remains to be done. This will be the initial context for the new session."`
}

const NewSessionToolName = "new_session"

// NewSessionError is a special sentinel error type that signals to the coordinator
// that a new session has been requested.
type NewSessionError struct {
	Summary string
}

func (e *NewSessionError) Error() string {
	return "new session requested"
}

//go:embed new_session.md.tpl
var newSessionDescriptionTmpl []byte

var newSessionDescriptionTpl = template.Must(
	template.New("newSessionDescription").
		Parse(string(newSessionDescriptionTmpl)),
)

type newSessionDescriptionData struct {
	ContextStatusEnabled bool
	AutoSummarizeEnabled bool
}

func newSessionDescription(contextStatusEnabled, autoSummarizeEnabled bool) string {
	var out bytes.Buffer
	if err := newSessionDescriptionTpl.Execute(&out, newSessionDescriptionData{
		ContextStatusEnabled: contextStatusEnabled,
		AutoSummarizeEnabled: autoSummarizeEnabled,
	}); err != nil {
		// This should never happen.
		panic("failed to execute new_session description template: " + err.Error())
	}
	return out.String()
}

func NewNewSessionTool(contextStatusEnabled, autoSummarizeEnabled bool) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		NewSessionToolName,
		newSessionDescription(contextStatusEnabled, autoSummarizeEnabled),
		func(ctx context.Context, params NewSessionParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if strings.TrimSpace(params.Summary) == "" {
				return fantasy.ToolResponse{
					Content: "A non-empty summary is required to create a new session.",
					IsError: true,
				}, nil
			}
			return fantasy.ToolResponse{}, &NewSessionError{Summary: params.Summary}
		},
	)
}
