package agent

import (
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// TestBuildToolsNewSessionRegistration verifies that buildTools registers the
// new_session tool for every top-level agent, omits it for sub-agents (which
// cannot perform the TUI handoff), and picks the description variant that
// matches whether context status injection is active (llm compaction mode
// only) and whether backend auto-summarization is enabled (always in llm
// mode; honoring disable_auto_summarize otherwise).
//
// Interactivity is deliberately not a factor here. One agent serves an
// attached TUI and headless `crush run` prompts at the same time, so the
// tool stays in the palette and sessionAgent.turnTools withholds it from
// the turns that cannot perform the handoff. That half is pinned by
// TestRun_NonInteractiveTurnWithholdsInteractiveTools.
func TestBuildToolsNewSessionRegistration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		method               config.CompactionMethod
		isSubAgent           bool
		disableAutoSummarize bool
		wantTool             bool
		wantCtxStatus        bool
		wantAutoSummarizeOff bool
	}{
		{
			name:          "llm mode",
			method:        config.CompactionLLM,
			wantTool:      true,
			wantCtxStatus: true,
		},
		{
			name:     "auto mode",
			method:   config.CompactionAuto,
			wantTool: true,
		},
		{
			name:                 "auto mode auto-summarize disabled",
			method:               config.CompactionAuto,
			disableAutoSummarize: true,
			wantTool:             true,
			wantAutoSummarizeOff: true,
		},
		{
			name:       "llm mode sub-agent",
			method:     config.CompactionLLM,
			isSubAgent: true,
		},
		{
			name:       "auto mode sub-agent",
			method:     config.CompactionAuto,
			isSubAgent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := testEnv(t)
			cfg, err := config.Init(env.workingDir, "", false)
			require.NoError(t, err)
			cfg.Config().Options.CompactionMethod = tc.method
			cfg.Config().Options.DisableAutoSummarize = tc.disableAutoSummarize

			coord := &coordinator{
				cfg:         cfg,
				sessions:    env.sessions,
				messages:    env.messages,
				permissions: env.permissions,
				history:     env.history,
				filetracker: *env.filetracker,
			}

			// A minimal agent config; its AllowedTools deliberately excludes
			// "agent" so buildTools skips the sub-agent tool path, which would
			// require configured models and providers.
			agentCfg := config.Agent{
				ID:           "test",
				Name:         "Test",
				Model:        config.SelectedModelTypeLarge,
				AllowedTools: []string{"bash", tools.NewSessionToolName},
				AllowedMCP:   map[string][]string{},
			}

			builtTools, err := coord.buildTools(t.Context(), agentCfg, tc.isSubAgent)
			require.NoError(t, err)

			var newSessionTool fantasy.AgentTool
			for _, tool := range builtTools {
				if tool.Info().Name == tools.NewSessionToolName {
					newSessionTool = tool
					break
				}
			}

			if !tc.wantTool {
				require.Nil(t, newSessionTool, "new_session should not be registered")
				return
			}
			require.NotNil(t, newSessionTool, "new_session should be registered")

			desc := newSessionTool.Info().Description
			if tc.wantCtxStatus {
				require.Contains(t, desc, "used_pct")
				require.NotContains(t, desc, "only when the user instructs you to")
				return
			}
			require.NotContains(t, desc, "used_pct")
			if tc.wantAutoSummarizeOff {
				require.Contains(t, desc, "when you judge the conversation has grown long enough")
				require.NotContains(t, desc, "only when the user instructs you to")
			} else {
				require.Contains(t, desc, "only when the user instructs you to")
				require.NotContains(t, desc, "when you judge the conversation has grown long enough")
			}
		})
	}
}
