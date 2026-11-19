package config

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestRealVeniceModelsClientGetMapsModelsAndSendsHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"data": [
				{
					"id": "venice-reasoning",
					"type": "text",
					"model_spec": {
						"name": "Venice Reasoning",
						"availableContextTokens": 128000,
						"maxCompletionTokens": 4096,
						"offline": false,
						"pricing": {
							"input": { "usd": "0.15" },
							"output": { "usd": 0.6 },
							"cache_input": { "usd": "0.03" }
						},
						"capabilities": {
							"supportsReasoning": true,
							"reasoningEffortOptions": ["low", "medium", "high"],
							"defaultReasoningEffort": "medium",
							"supportsVision": true
						}
					}
				},
				{
					"id": "venice-fallback",
					"type": "text",
					"context_length": 64000,
					"model_spec": {
						"name": "Venice Fallback",
						"maxCompletionTokens": 2048,
						"offline": false
					}
				},
				{
					"id": "venice-image",
					"type": "image",
					"model_spec": { "name": "Image Model" }
				},
				{
					"id": "venice-offline",
					"type": "text",
					"model_spec": { "name": "Offline Model", "offline": true }
				}
			]
		}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	client := realVeniceModelsClient{baseURL: server.URL + "/", apiKey: " test-key "}
	provider, err := client.Get(t.Context())

	require.NoError(t, err)
	require.Equal(t, catwalk.InferenceProviderVenice, provider.ID)
	require.Equal(t, server.URL, provider.APIEndpoint)
	require.Equal(t, catwalk.TypeCompletions, provider.Type)
	require.Equal(t, []catwalk.Model{
		{
			ID:               "venice-reasoning",
			Name:             "Venice Reasoning",
			Pricing:          catwalk.Pricing{Input: 0.15, Output: 0.6, CacheCreate: 0.03},
			ContextWindow:    128000,
			DefaultMaxTokens: 4096,
			Reasoning: catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels("low", "medium", "high"),
				DefaultEffortLevel: "medium",
			},
			Capabilities: catwalk.Capabilities{Vision: true},
		},
		{
			ID:               "venice-fallback",
			Name:             "Venice Fallback",
			ContextWindow:    64000,
			DefaultMaxTokens: 2048,
			Reasoning:        catwalk.Reasoning{Thinking: catwalk.ThinkingNever},
		},
	}, provider.Models)
}

func TestVeniceModelsToCatwalkModelsFiltersNonTextAndOfflineModels(t *testing.T) {
	t.Parallel()

	models := veniceModelsToCatwalkModels(veniceModelsResponse{Data: []veniceModel{
		{
			ID:   "text-model",
			Type: "TEXT",
			ModelSpec: veniceModelSpec{
				Name:                   "Text Model",
				AvailableContextTokens: 32000,
				MaxCompletionTokens:    2048,
				Pricing: veniceModelPricing{
					Input:      veniceModelPricingValue{USD: veniceUSD(0.1)},
					Output:     veniceModelPricingValue{USD: veniceUSD(0.2)},
					CacheInput: veniceModelPricingValue{USD: veniceUSD(0.01)},
				},
				Capabilities: veniceModelCapabilities{
					SupportsReasoning:      true,
					ReasoningEffortOptions: []string{"low"},
					DefaultReasoningEffort: "low",
				},
			},
		},
		{
			ID:        "image-model",
			Type:      "image",
			ModelSpec: veniceModelSpec{Name: "Image Model"},
		},
		{
			ID:        "offline-model",
			Type:      "text",
			ModelSpec: veniceModelSpec{Name: "Offline Model", Offline: true},
		},
	}})

	require.Equal(t, []catwalk.Model{
		{
			ID:               "text-model",
			Name:             "Text Model",
			Pricing:          catwalk.Pricing{Input: 0.1, Output: 0.2, CacheCreate: 0.01},
			ContextWindow:    32000,
			DefaultMaxTokens: 2048,
			Reasoning: catwalk.Reasoning{
				Thinking:           catwalk.ThinkingToggleable,
				EffortLevels:       catwalk.NewEffortLevels("low"),
				DefaultEffortLevel: "low",
			},
		},
	}, models)
}
