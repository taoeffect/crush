package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEffectiveToolNames pins which configured tools an agent is built
// with. A main agent keeps every one, and nothing the config leaves out
// reappears; each turn then drops the tools its caller cannot use (see
// sessionAgent.turnTools). A subagent can neither fan out nor ask, so it
// loses the delegation tools and the question tool.
func TestEffectiveToolNames(t *testing.T) {
	t.Parallel()

	agentCfg := config.Agent{
		AllowedTools: []string{
			AgentToolName,
			AgentDispatchToolName,
			AgentSendToolName,
			AgentStatusToolName,
			AgentStopToolName,
			"question",
			"view",
		},
	}

	tests := []struct {
		name       string
		isSubAgent bool
		want       []string
	}{
		{
			name: "main agents keep every configured tool",
			want: []string{
				AgentToolName,
				AgentDispatchToolName,
				AgentSendToolName,
				AgentStatusToolName,
				AgentStopToolName,
				"question",
				"view",
			},
		},
		{
			name:       "subagents neither fan out nor ask",
			isSubAgent: true,
			want:       []string{"view"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, effectiveToolNames(agentCfg, tt.isSubAgent))
		})
	}
}

// TestResolveSubAgentAccess covers how a requested access level maps onto a
// subagent variant, including plan mode, which may only delegate work it
// could have done itself.
func TestResolveSubAgentAccess(t *testing.T) {
	t.Parallel()

	read := &mockSessionAgent{}
	write := &mockSessionAgent{}
	both := map[string]SessionAgent{accessRead: read, accessWrite: write}
	readOnly := map[string]SessionAgent{accessRead: read}

	t.Run("empty access defaults to read", func(t *testing.T) {
		t.Parallel()
		agent, refusal := resolveSubAgent(both, "", false)
		require.Empty(t, refusal)
		assert.Same(t, read, agent)
	})

	t.Run("write is available to a writing parent", func(t *testing.T) {
		t.Parallel()
		agent, refusal := resolveSubAgent(both, accessWrite, false)
		require.Empty(t, refusal)
		assert.Same(t, write, agent)
	})

	t.Run("plan mode refuses write with a reason", func(t *testing.T) {
		t.Parallel()
		agent, refusal := resolveSubAgent(readOnly, accessWrite, true)
		assert.Nil(t, agent)
		assert.Contains(t, refusal, "read-only")
	})

	t.Run("plan mode still delegates reads", func(t *testing.T) {
		t.Parallel()
		agent, refusal := resolveSubAgent(readOnly, accessRead, true)
		require.Empty(t, refusal)
		assert.Same(t, read, agent)
	})

	t.Run("nonsense access is rejected", func(t *testing.T) {
		t.Parallel()
		agent, refusal := resolveSubAgent(both, "admin", false)
		assert.Nil(t, agent)
		assert.Contains(t, refusal, `"read" or "write"`)
	})
}

// TestReadOnlyParent documents which parents may only launch read-only
// subagents.
func TestReadOnlyParent(t *testing.T) {
	t.Parallel()

	assert.True(t, readOnlyParent(config.AgentPlan))
	assert.False(t, readOnlyParent(config.AgentCoder))
	assert.False(t, readOnlyParent(""))
}
