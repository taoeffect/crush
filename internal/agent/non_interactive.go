package agent

import "context"

// nonInteractiveContextKey is the unexported context key that carries
// "no human is watching this run". The boundary that starts a run
// (backend.runAgent for the client/server path, app.RunNonInteractive
// for the in-process one) sets it, and coordinator.run copies it onto
// SessionAgentCall.NonInteractive. Each turn then sets it again on its
// run context from its own call (withNonInteractive), so the turn's tools
// read the value of the prompt they serve.
type nonInteractiveContextKey struct{}

// WithNonInteractive returns ctx tagged as a run nobody can answer
// prompts for: `crush run`, in both the in-process and the client/server
// path.
//
// Two things depend on it. The run waits for MCP initialization to
// settle, because it gets a single shot at the tool palette and cannot
// pick up a late server on a later prompt. And interactive-only tools
// are withheld from its turn: a call to one would block until the run
// was cancelled, since nobody is there to answer.
//
// It is a property of the run, not of the workspace. One workspace on
// the shared server serves an attached TUI and headless prompts at the
// same time, so the coordinator cannot answer this question for
// everyone.
func WithNonInteractive(ctx context.Context) context.Context {
	return withNonInteractive(ctx, true)
}

// withNonInteractive sets ctx's interactivity to nonInteractive. A turn
// stamps its own call's value onto its run context, so a prompt
// dequeued after a headless turn does not inherit its tag, nor the
// reverse.
func withNonInteractive(ctx context.Context, nonInteractive bool) context.Context {
	return context.WithValue(ctx, nonInteractiveContextKey{}, nonInteractive)
}

// NonInteractiveFromContext reports whether ctx is marked as a run that
// nobody can answer prompts for. [WithNonInteractive] sets the mark, and
// each turn sets it again from its own call, to true or false. A ctx
// with no mark is interactive. Exported because the boundary packages
// and their tests need to read it; safe to call on any context.
func NonInteractiveFromContext(ctx context.Context) bool {
	nonInteractive, _ := ctx.Value(nonInteractiveContextKey{}).(bool)
	return nonInteractive
}
