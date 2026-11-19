package agent

import (
	"context"
	_ "embed"
	"errors"
	"slices"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
)

//go:embed templates/agent_tool.md
var agentToolDescription string

type AgentParams struct {
	Prompt string `json:"prompt" description:"The task for the agent to perform"`
	Access string `json:"access,omitempty" description:"\"read\" (default) for a subagent that can only inspect the codebase, or \"write\" for one that can also edit files"`
}

const (
	AgentToolName = "agent"

	accessRead  = "read"
	accessWrite = "write"
)

// writeAgentConfig derives the read-write variant of the task agent: the same
// agent, given the coder's tools. Subagents never get the delegation tools
// or the question tool; effectiveToolNames strips them.
func writeAgentConfig(cfg *config.Config) config.Agent {
	task := cfg.Agents[config.AgentTask]
	if coder, ok := cfg.Agents[config.AgentCoder]; ok {
		task.AllowedTools = slices.Clone(coder.AllowedTools)
	}
	return task
}

// accessOrRead defaults an unset access level to read-only.
func accessOrRead(access string) string {
	if access == "" {
		return accessRead
	}
	return access
}

// readOnlyParent reports whether a parent agent may only delegate to
// read-only subagents. Plan mode promises it cannot change files, and that
// promise has to cover the work it hands off as well as the work it does
// itself.
func readOnlyParent(parentAgentID string) bool {
	return parentAgentID == config.AgentPlan
}

// resolveSubAgent picks the subagent variant for a requested access level,
// returning a message for the model when there is no such variant.
func resolveSubAgent(agents map[string]SessionAgent, access string, readOnly bool) (SessionAgent, string) {
	access = accessOrRead(access)
	if agent, ok := agents[access]; ok {
		return agent, ""
	}
	if readOnly && access == accessWrite {
		return nil, `plan mode subagents are read-only; omit access or pass "read"`
	}
	return nil, `access must be "read" or "write"`
}

// taskAgents builds the variants of the task subagent. Both the synchronous
// agent tool and the detached dispatch tool draw from these. A read-only
// parent gets only the read variant, so the write one is never built rather
// than built and withheld.
func (c *coordinator) taskAgents(ctx context.Context, readOnly bool) (map[string]SessionAgent, error) {
	if _, ok := c.cfg.Config().Agents[config.AgentTask]; !ok {
		return nil, errors.New("task agent not configured")
	}
	accesses := []string{accessRead, accessWrite}
	if readOnly {
		accesses = accesses[:1]
	}

	// Each variant is fully set up before this returns. This runs on every
	// turn, so the variants are never registered as main agents.
	agents := make(map[string]SessionAgent, len(accesses))
	readies := make([]*agentReadiness, 0, len(accesses))
	for _, access := range accesses {
		agentCfg, canWrite := c.taskAgentConfig(access)
		agent, ready, err := c.buildTaskAgent(ctx, agentCfg, canWrite, nil)
		if err != nil {
			return nil, err
		}
		agents[access] = agent
		readies = append(readies, ready)
	}
	for _, ready := range readies {
		if err := ready.wait(ctx); err != nil {
			return nil, err
		}
	}
	return agents, nil
}

// taskAgentConfig returns the task agent's config for an access level, and
// whether it may write.
func (c *coordinator) taskAgentConfig(access string) (config.Agent, bool) {
	if access == accessWrite {
		return writeAgentConfig(c.cfg.Config()), true
	}
	return c.cfg.Config().Agents[config.AgentTask], false
}

// buildTaskAgent builds one variant of the task subagent and returns the
// handle for its setup, so several variants can build at the same time.
// modelsFn, when set, replaces the shared subagent model set; a dispatch
// with a model override uses it. The variant renders a prompt of its own
// for agent_dispatch and agent_send; a blocking agent call brings one
// rendered for its run's model.
func (c *coordinator) buildTaskAgent(ctx context.Context, agentCfg config.Agent, canWrite bool, modelsFn func() ModelSet) (SessionAgent, *agentReadiness, error) {
	pr, err := c.taskPrompt(canWrite)
	if err != nil {
		return nil, nil, err
	}
	return c.buildAgentWithModels(ctx, pr, agentCfg, true, modelsFn)
}

// taskPrompt returns the task subagent's prompt for one access level.
func (c *coordinator) taskPrompt(canWrite bool) (*prompt.Prompt, error) {
	return c.newPrompt("task",
		prompt.WithWorkingDir(c.cfg.WorkingDir()),
		prompt.WithCanWrite(canWrite),
	)
}

func (c *coordinator) agentTool(ctx context.Context, parentAgentID string) (fantasy.AgentTool, error) {
	readOnly := readOnlyParent(parentAgentID)
	agents, err := c.taskAgents(ctx, readOnly)
	if err != nil {
		return nil, err
	}
	// Parse the templates once per turn, not on every delegated call.
	prompts := make(map[string]*prompt.Prompt, len(agents))
	for access := range agents {
		pr, err := c.taskPrompt(access == accessWrite)
		if err != nil {
			return nil, err
		}
		prompts[access] = pr
	}
	return fantasy.NewParallelAgentTool(
		AgentToolName,
		agentToolDescription,
		func(ctx context.Context, params AgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Prompt == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			agent, refusal := resolveSubAgent(agents, params.Access, readOnly)
			if refusal != "" {
				return fantasy.NewTextErrorResponse(refusal), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}

			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			// The delegated turn runs on the models of the run that
			// asked for it, rebuilt with sub-agent provider settings.
			models, err := c.subAgentRunModels(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			// The prompt is rendered for the provider and model this
			// turn will talk to, which is not necessarily the
			// workspace's. It goes on the call rather than on the
			// shared sub-agent instance, so concurrent parent runs
			// cannot overwrite each other's.
			pr := prompts[accessOrRead(params.Access)]
			large := models.Large()
			systemPrompt, err := pr.Build(ctx, large.Model.Provider(), large.Model.Model(), c.cfg)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			return c.runSubAgentMaybeBackground(ctx, AgentToolName, params.Access, subAgentParams{
				Agent:          agent,
				Models:         models,
				SystemPrompt:   systemPrompt,
				SessionID:      sessionID,
				AgentMessageID: agentMessageID,
				ToolCallID:     call.ID,
				Prompt:         params.Prompt,
				SessionTitle:   "New Agent Session",
				// The delegated turn is part of this run, so it needs
				// the same approval. Without it a `crush run` blocks
				// forever on the child session's first permission
				// request: nobody is there to answer it.
				AutoApprove: AutoApproveFromContext(ctx),
			})
		},
	), nil
}
