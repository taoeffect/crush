package config

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestConfig_ValidateReasoningEffort(t *testing.T) {
	t.Parallel()

	newConfig := func(models ...catwalk.Model) *Config {
		return &Config{
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID:     "openai",
					Models: models,
				},
			}),
		}
	}

	t.Run("supported effort passes", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig(catwalk.Model{
			ID:              "gpt-5",
			CanReason:       true,
			ReasoningLevels: []string{"low", "medium", "high"},
		})
		require.NoError(t, cfg.ValidateReasoningEffort("openai", "gpt-5", "high"))
	})

	t.Run("unsupported effort lists accepted values", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig(catwalk.Model{
			ID:              "gpt-5",
			CanReason:       true,
			ReasoningLevels: []string{"low", "medium", "high"},
		})
		err := cfg.ValidateReasoningEffort("openai", "gpt-5", "ultra")
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not support reasoning effort")
		require.Contains(t, err.Error(), `"ultra"`)
		require.Contains(t, err.Error(), "accepted values: low, medium, high")
	})

	t.Run("model without reasoning levels rejects any effort", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig(catwalk.Model{ID: "gpt-4o"})
		err := cfg.ValidateReasoningEffort("openai", "gpt-4o", "low")
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not support reasoning effort")
	})

	t.Run("unknown model errors", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig(catwalk.Model{ID: "gpt-4o"})
		err := cfg.ValidateReasoningEffort("openai", "gpt-5", "low")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("empty effort is rejected like any other value", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig(catwalk.Model{
			ID:              "gpt-5",
			CanReason:       true,
			ReasoningLevels: []string{"low", "medium", "high"},
		})
		require.Error(t, cfg.ValidateReasoningEffort("openai", "gpt-5", ""))
	})
}

// TestConfig_SelectionWithReasoningEffort pins the run-local contract of
// --reasoning-effort: the flag qualifies one run's model, so it is
// validated against the model that run will use and never written back
// to the workspace, where it would outlive the command and follow every
// other client in the directory.
func TestConfig_SelectionWithReasoningEffort(t *testing.T) {
	t.Parallel()

	newConfig := func() *Config {
		return &Config{
			Models: map[SelectedModelType]SelectedModel{
				SelectedModelTypeLarge: {Provider: "openai", Model: "gpt-5"},
			},
			Providers: csync.NewMapFrom(map[string]ProviderConfig{
				"openai": {
					ID: "openai",
					Models: []catwalk.Model{{
						ID:              "gpt-5",
						CanReason:       true,
						ReasoningLevels: []string{"low", "medium", "high"},
					}},
				},
				"anthropic": {
					ID:     "anthropic",
					Models: []catwalk.Model{{ID: "claude"}},
				},
			}),
		}
	}

	t.Run("validates against the run's model, not the workspace default", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig()
		// The workspace default accepts "high"; the model this run
		// restored does not reason at all.
		_, err := cfg.SelectionWithReasoningEffort(
			&SelectedModel{Provider: "anthropic", Model: "claude"}, "high")
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not support reasoning effort")
	})

	t.Run("keeps the run's model and leaves the caller's selection alone", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig()
		run := &SelectedModel{Provider: "openai", Model: "gpt-5"}
		got, err := cfg.SelectionWithReasoningEffort(run, "low")
		require.NoError(t, err)
		require.Equal(t, SelectedModel{Provider: "openai", Model: "gpt-5", ReasoningEffort: "low"}, *got)
		require.Empty(t, run.ReasoningEffort, "the caller's selection must not be rewritten")
	})

	t.Run("falls back to the workspace model without writing to it", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig()
		got, err := cfg.SelectionWithReasoningEffort(nil, "medium")
		require.NoError(t, err)
		require.Equal(t, SelectedModel{Provider: "openai", Model: "gpt-5", ReasoningEffort: "medium"}, *got)
		require.Empty(t, cfg.Models[SelectedModelTypeLarge].ReasoningEffort,
			"one run's effort must not become the workspace default")
	})

	t.Run("errors when nothing selects a large model", func(t *testing.T) {
		t.Parallel()
		cfg := newConfig()
		cfg.Models = nil
		_, err := cfg.SelectionWithReasoningEffort(nil, "low")
		require.Error(t, err)
		require.Contains(t, err.Error(), "--model")
	})
}
