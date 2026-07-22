package agent

import (
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// TestBuildToolsNewSessionRegistration verifies that buildTools registers the
// new_session tool for interactive top-level agents in both compaction modes,
// omits it for non-interactive coordinators and for sub-agents (which cannot
// perform the TUI handoff), and picks the description variant that matches
// whether context status injection is active (llm compaction mode only) and
// whether backend auto-summarization is enabled (always in llm mode; honoring
// disable_auto_summarize otherwise).
func TestBuildToolsNewSessionRegistration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		interactive          bool
		method               config.CompactionMethod
		isSubAgent           bool
		disableAutoSummarize bool
		wantTool             bool
		wantCtxStatus        bool
		wantAutoSummarizeOff bool
	}{
		{
			name:          "interactive llm mode",
			interactive:   true,
			method:        config.CompactionLLM,
			wantTool:      true,
			wantCtxStatus: true,
		},
		{
			name:        "interactive auto mode",
			interactive: true,
			method:      config.CompactionAuto,
			wantTool:    true,
		},
		{
			name:                 "interactive auto mode auto-summarize disabled",
			interactive:          true,
			method:               config.CompactionAuto,
			disableAutoSummarize: true,
			wantTool:             true,
			wantAutoSummarizeOff: true,
		},
		{
			name:        "interactive llm mode sub-agent",
			interactive: true,
			method:      config.CompactionLLM,
			isSubAgent:  true,
		},
		{
			name:        "interactive auto mode sub-agent",
			interactive: true,
			method:      config.CompactionAuto,
			isSubAgent:  true,
		},
		{
			name:   "non-interactive llm mode",
			method: config.CompactionLLM,
		},
		{
			name:   "non-interactive auto mode",
			method: config.CompactionAuto,
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
				interactive: tc.interactive,
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
