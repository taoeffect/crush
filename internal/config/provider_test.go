package config

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func resetProviderState() {
	providerOnce = sync.Once{}
	providerMu.Lock()
	providerList = nil
	providerErr = nil
	pendingProvider = nil
	providerMu.Unlock()
	catwalkSyncer = &catwalkSync{}
	hyperSyncer = &hyperSync{}
	veniceSyncer = &liveProviderSync{}
	copilotSyncer = &liveProviderSync{}
}

func TestProviders_Integration_AutoUpdateDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Use a test-specific instance to avoid global state interference.
	testCatwalkSyncer := &catwalkSync{}
	testHyperSyncer := &hyperSync{}

	originalCatwalSyncer := catwalkSyncer
	originalHyperSyncer := hyperSyncer
	defer func() {
		catwalkSyncer = originalCatwalSyncer
		hyperSyncer = originalHyperSyncer
	}()

	catwalkSyncer = testCatwalkSyncer
	hyperSyncer = testHyperSyncer

	resetProviderState()
	defer resetProviderState()

	cfg := &Config{
		Options: &Options{
			DisableProviderAutoUpdate: true,
		},
	}

	providers, err := Providers(cfg)
	require.NoError(t, err)
	require.NotNil(t, providers)
	require.Greater(t, len(providers), 5, "Expected embedded providers")
}

func TestProviders_Integration_WithMockClients(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Create fresh syncers for this test.
	testCatwalkSyncer := &catwalkSync{}
	testHyperSyncer := &hyperSync{}

	// Initialize with mock clients.
	mockCatwalkClient := &mockCatwalkClient{
		providers: []catwalk.Provider{
			{Name: "Provider1", ID: "p1"},
			{Name: "Provider2", ID: "p2"},
		},
	}
	mockHyperClient := &mockHyperClient{
		provider: catwalk.Provider{
			Name: "Hyper",
			ID:   "hyper",
			Models: []catwalk.Model{
				{ID: "hyper-1", Name: "Hyper Model"},
			},
		},
	}

	catwalkPath := tmpDir + "/crush/providers.json"
	hyperPath := tmpDir + "/crush/hyper.json"

	testCatwalkSyncer.Init(mockCatwalkClient, catwalkPath, true)
	testHyperSyncer.Init(mockHyperClient, hyperPath, true)

	// Get providers from each syncer.
	catwalkProviders, err := testCatwalkSyncer.Get(t.Context())
	require.NoError(t, err)
	require.Len(t, catwalkProviders, 2)

	hyperProvider, err := testHyperSyncer.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Hyper", hyperProvider.Name)

	// Verify total.
	allProviders := append(catwalkProviders, hyperProvider)
	require.Len(t, allProviders, 3)
}

func TestProviders_Integration_WithCachedData(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Create cache files.
	catwalkPath := tmpDir + "/crush/providers.json"
	hyperPath := tmpDir + "/crush/hyper.json"

	require.NoError(t, os.MkdirAll(tmpDir+"/crush", 0o755))

	// Write Catwalk cache.
	catwalkProviders := []catwalk.Provider{
		{Name: "Cached1", ID: "c1"},
		{Name: "Cached2", ID: "c2"},
	}
	data, err := json.Marshal(catwalkProviders)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(catwalkPath, data, 0o644))

	// Write Hyper cache.
	hyperProvider := catwalk.Provider{
		Name: "Cached Hyper",
		ID:   "hyper",
	}
	data, err = json.Marshal(hyperProvider)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(hyperPath, data, 0o644))

	// Create fresh syncers.
	testCatwalkSyncer := &catwalkSync{}
	testHyperSyncer := &hyperSync{}

	// Mock clients that return ErrNotModified.
	mockCatwalkClient := &mockCatwalkClient{
		err: catwalk.ErrNotModified,
	}
	mockHyperClient := &mockHyperClient{
		err: catwalk.ErrNotModified,
	}

	testCatwalkSyncer.Init(mockCatwalkClient, catwalkPath, true)
	testHyperSyncer.Init(mockHyperClient, hyperPath, true)

	// Get providers - should use cached.
	catwalkResult, err := testCatwalkSyncer.Get(t.Context())
	require.NoError(t, err)
	require.Len(t, catwalkResult, 2)
	require.Equal(t, "Cached1", catwalkResult[0].Name)

	hyperResult, err := testHyperSyncer.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Cached Hyper", hyperResult.Name)
}

func TestProviders_Integration_CatwalkFailsHyperSucceeds(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	testCatwalkSyncer := &catwalkSync{}
	testHyperSyncer := &hyperSync{}

	// Catwalk fails, Hyper succeeds.
	mockCatwalkClient := &mockCatwalkClient{
		err: catwalk.ErrNotModified, // Will use embedded.
	}
	mockHyperClient := &mockHyperClient{
		provider: catwalk.Provider{
			Name: "Hyper",
			ID:   "hyper",
			Models: []catwalk.Model{
				{ID: "hyper-1", Name: "Hyper Model"},
			},
		},
	}

	catwalkPath := tmpDir + "/crush/providers.json"
	hyperPath := tmpDir + "/crush/hyper.json"

	testCatwalkSyncer.Init(mockCatwalkClient, catwalkPath, true)
	testHyperSyncer.Init(mockHyperClient, hyperPath, true)

	catwalkResult, err := testCatwalkSyncer.Get(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, catwalkResult) // Should have embedded.

	hyperResult, err := testHyperSyncer.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Hyper", hyperResult.Name)
}

func TestProviders_Integration_BothFail(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	testCatwalkSyncer := &catwalkSync{}
	testHyperSyncer := &hyperSync{}

	// Both fail.
	mockCatwalkClient := &mockCatwalkClient{
		err: catwalk.ErrNotModified,
	}
	mockHyperClient := &mockHyperClient{
		provider: catwalk.Provider{}, // Empty provider.
	}

	catwalkPath := tmpDir + "/crush/providers.json"
	hyperPath := tmpDir + "/crush/hyper.json"

	testCatwalkSyncer.Init(mockCatwalkClient, catwalkPath, true)
	testHyperSyncer.Init(mockHyperClient, hyperPath, true)

	catwalkResult, err := testCatwalkSyncer.Get(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, catwalkResult) // Should fall back to embedded.

	hyperResult, err := testHyperSyncer.Get(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Charm Hyper", hyperResult.Name) // Falls back to embedded when no models.
}

func TestProviders_Integration_LiveOverlayRefreshesWithCredentialsInBackground(t *testing.T) {
	resetProviderState()
	defer resetProviderState()

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/models", r.URL.Path)
		requestCount.Add(1)
		started <- struct{}{}
		<-release

		switch r.Header.Get("Authorization") {
		case "Bearer venice-token":
			_, _ = w.Write([]byte(`{
				"data": [
					{
						"id": "venice-live",
						"type": "text",
						"model_spec": {
							"name": "Venice Live",
							"availableContextTokens": 4096,
							"maxCompletionTokens": 1024,
							"pricing": {"input": {"usd": "0.1"}, "output": {"usd": "0.2"}},
							"capabilities": {"supportsVision": true}
						}
						}
				]
			}`))
		case "Bearer copilot-token":
			_, _ = w.Write([]byte(`{
				"data": [
					{
						"id": "copilot-live",
						"name": "Copilot Live",
						"capabilities": {
							"limits": {"max_context_window_tokens": 8192, "max_output_tokens": 2048},
							"supports": {"vision": true}
						}
					}
				]
			}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	providers := []catwalk.Provider{
		{
			Name:                "Venice",
			ID:                  catwalk.InferenceProviderVenice,
			APIEndpoint:         server.URL,
			Type:                catwalk.TypeOpenAICompat,
			DefaultLargeModelID: "venice-live",
			DefaultSmallModelID: "venice-live",
			Models:              []catwalk.Model{{ID: "venice-seed", Name: "Venice Seed"}},
		},
		{
			Name:                "Copilot",
			ID:                  catwalk.InferenceProviderCopilot,
			APIEndpoint:         server.URL,
			Type:                catwalk.TypeOpenAICompat,
			DefaultLargeModelID: "copilot-live",
			DefaultSmallModelID: "copilot-live",
			Models:              []catwalk.Model{{ID: "copilot-seed", Name: "Copilot Seed"}},
		},
	}
	cfg := &Config{
		Options: &Options{},
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			string(catwalk.InferenceProviderVenice):  {APIKey: "venice-token"},
			string(catwalk.InferenceProviderCopilot): {APIKey: "copilot-token"},
		}),
	}

	resultCh := make(chan []catwalk.Provider, 1)
	go func() {
		resultCh <- overlayLiveProviderModels(t.Context(), cfg, providers, true)
	}()

	var result []catwalk.Provider
	select {
	case result = <-resultCh:
	case <-time.After(100 * time.Millisecond):
		close(release)
		require.FailNow(t, "Live overlay blocked on background refresh")
	}

	venice, ok := findProvider(result, catwalk.InferenceProviderVenice)
	require.True(t, ok)
	require.Equal(t, []catwalk.Model{{ID: "venice-seed", Name: "Venice Seed"}}, venice.Models)

	copilot, ok := findProvider(result, catwalk.InferenceProviderCopilot)
	require.True(t, ok)
	require.Equal(t, []catwalk.Model{{ID: "copilot-seed", Name: "Copilot Seed"}}, copilot.Models)

	require.Eventually(t, func() bool { return requestCount.Load() == 2 }, time.Second, 10*time.Millisecond)
	<-started
	<-started
	close(release)

	require.Eventually(t, func() bool {
		venice, _, err := newCache[catwalk.Provider](cachePathFor("venice")).Get()
		return err == nil && len(venice.Models) == 1 && venice.Models[0].ID == "venice-live"
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		copilot, _, err := newCache[catwalk.Provider](cachePathFor("copilot")).Get()
		return err == nil && len(copilot.Models) == 1 && copilot.Models[0].ID == "copilot-live"
	}, time.Second, 10*time.Millisecond)
}

func TestProviders_Integration_LiveOverlayUpdatesProviderListAndPublishesEvent(t *testing.T) {
	resetProviderState()
	defer resetProviderState()

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/models", r.URL.Path)
		started <- struct{}{}
		<-release
		require.Equal(t, "Bearer venice-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"id": "venice-live",
					"type": "text",
					"model_spec": {
						"name": "Venice Live",
						"availableContextTokens": 4096,
						"maxCompletionTokens": 1024,
						"pricing": {"input": {"usd": "0.1"}, "output": {"usd": "0.2"}},
						"capabilities": {"supportsVision": true}
					}
				}
			]
		}`))
	}))
	defer server.Close()

	seed := catwalk.Provider{
		Name:                "Venice",
		ID:                  catwalk.InferenceProviderVenice,
		APIEndpoint:         server.URL,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "venice-seed",
		DefaultSmallModelID: "venice-seed",
		Models:              []catwalk.Model{{ID: "venice-seed", Name: "Venice Seed"}},
	}
	cfg := &Config{
		Options: &Options{},
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			string(catwalk.InferenceProviderVenice): {APIKey: "venice-token"},
		}),
	}
	providerOnce.Do(func() {})
	setProviderList([]catwalk.Provider{seed})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := SubscribeProviderEvents(ctx)

	result := overlayLiveProviderModels(t.Context(), cfg, []catwalk.Provider{seed}, true)
	require.Equal(t, []catwalk.Model{{ID: "venice-seed", Name: "Venice Seed"}}, result[0].Models)
	<-started
	close(release)

	select {
	case event := <-events:
		require.Equal(t, pubsub.UpdatedEvent, event.Type)
		require.Equal(t, string(catwalk.InferenceProviderVenice), event.Payload.ProviderID)
	case <-time.After(time.Second):
		require.FailNow(t, "Timed out waiting for providers updated event")
	}

	providers, err := Providers(cfg)
	require.NoError(t, err)
	venice, ok := findProvider(providers, catwalk.InferenceProviderVenice)
	require.True(t, ok)
	require.Equal(t, []catwalk.Model{{
		ID:               "venice-live",
		Name:             "Venice Live",
		CostPer1MIn:      0.1,
		CostPer1MOut:     0.2,
		ContextWindow:    4096,
		DefaultMaxTokens: 1024,
		SupportsImages:   true,
	}}, venice.Models)
}

func TestProviders_Integration_LiveOverlaySkipsWithoutCredentials(t *testing.T) {
	resetProviderState()
	defer resetProviderState()

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	seed := catwalk.Provider{
		Name:        "Venice",
		ID:          catwalk.InferenceProviderVenice,
		APIEndpoint: "http://127.0.0.1:1",
		Models:      []catwalk.Model{{ID: "seed-model", Name: "Seed Model"}},
	}
	cached := seed
	cached.Models = []catwalk.Model{{ID: "cached-model", Name: "Cached Model"}}
	require.NoError(t, newCache[catwalk.Provider](cachePathFor("venice")).Store(cached))

	cfg := &Config{Options: &Options{}, Providers: csync.NewMap[string, ProviderConfig]()}
	result := overlayLiveProviderModels(t.Context(), cfg, []catwalk.Provider{seed}, true)
	require.Equal(t, []catwalk.Provider{seed}, result)
}

func TestNewVeniceLiveProviderClient_EnvFallbackUsesResolver(t *testing.T) {
	t.Parallel()

	seed := catwalk.Provider{
		ID:          catwalk.InferenceProviderVenice,
		APIEndpoint: "https://api.venice.ai/api/v1",
	}
	cfg := &Config{Options: &Options{}, Providers: csync.NewMap[string, ProviderConfig]()}
	resolver := NewShellVariableResolver(env.NewFromMap(map[string]string{
		"VENICE_API_KEY": " env-venice-key ",
	}))

	client, credentialed, err := newVeniceLiveProviderClient(seed, cfg, resolver, "")
	require.NoError(t, err)
	require.True(t, credentialed)
	veniceClient, ok := client.(realVeniceModelsClient)
	require.True(t, ok)
	require.Equal(t, "env-venice-key", veniceClient.apiKey)
}

func TestUpdateVenice_LiveSourceStoresCache(t *testing.T) {
	resetProviderState()
	defer resetProviderState()

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/models", r.URL.Path)
		require.Equal(t, "Bearer venice-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{
			"data": [
				{
					"id": "venice-live",
					"type": "text",
					"model_spec": {
						"name": "Venice Live",
						"availableContextTokens": 4096,
						"maxCompletionTokens": 1024,
						"pricing": {"input": {"usd": "0.1"}, "output": {"usd": "0.2"}},
						"capabilities": {"supportsVision": true}
					}
				}
			]
		}`))
	}))
	defer server.Close()

	seed := catwalk.Provider{
		Name:                "Venice",
		ID:                  catwalk.InferenceProviderVenice,
		APIEndpoint:         server.URL,
		Type:                catwalk.TypeOpenAICompat,
		DefaultLargeModelID: "venice-live",
		DefaultSmallModelID: "venice-live",
	}
	require.NoError(t, newCache[[]catwalk.Provider](cachePathFor("providers")).Store([]catwalk.Provider{seed}))
	cfg := &Config{
		Options: &Options{},
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			string(catwalk.InferenceProviderVenice): {APIKey: "venice-token"},
		}),
	}

	require.NoError(t, UpdateVenice(server.URL, cfg))

	cached, _, err := newCache[catwalk.Provider](cachePathFor("venice")).Get()
	require.NoError(t, err)
	require.Equal(t, "Venice", cached.Name)
	require.Equal(t, "venice-live", cached.DefaultLargeModelID)
	require.Len(t, cached.Models, 1)
	require.Equal(t, "venice-live", cached.Models[0].ID)
}

func TestUpdateVenice_LiveSourceRequiresCredentials(t *testing.T) {
	resetProviderState()
	defer resetProviderState()

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	seed := catwalk.Provider{ID: catwalk.InferenceProviderVenice, Name: "Venice"}
	require.NoError(t, newCache[[]catwalk.Provider](cachePathFor("providers")).Store([]catwalk.Provider{seed}))
	cfg := &Config{Options: &Options{}, Providers: csync.NewMap[string, ProviderConfig]()}

	err := UpdateVenice("", cfg)
	require.Error(t, err)
	require.True(t, IsMissingLiveProviderCredentials(err))
}

func TestUpdateCopilot_FileSourceStoresCache(t *testing.T) {
	resetProviderState()
	defer resetProviderState()

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	provider := catwalk.Provider{
		Name: "Copilot",
		ID:   catwalk.InferenceProviderCopilot,
		Models: []catwalk.Model{
			{ID: "copilot-file", Name: "Copilot File"},
		},
	}
	data, err := json.Marshal(provider)
	require.NoError(t, err)
	path := filepath.Join(tmpDir, "copilot.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	require.NoError(t, UpdateCopilot(path, nil))

	cached, _, err := newCache[catwalk.Provider](cachePathFor("copilot")).Get()
	require.NoError(t, err)
	require.Equal(t, provider, cached)
}

func TestCache_StoreAndGet(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cachePath := tmpDir + "/test.json"

	cache := newCache[[]catwalk.Provider](cachePath)

	providers := []catwalk.Provider{
		{Name: "Provider1", ID: "p1"},
		{Name: "Provider2", ID: "p2"},
	}

	// Store.
	err := cache.Store(providers)
	require.NoError(t, err)

	// Get.
	result, etag, err := cache.Get()
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, "Provider1", result[0].Name)
	require.NotEmpty(t, etag)
}

func TestCache_GetNonExistent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cachePath := tmpDir + "/nonexistent.json"

	cache := newCache[[]catwalk.Provider](cachePath)

	_, _, err := cache.Get()
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read provider cache file")
}

func TestCache_GetInvalidJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cachePath := tmpDir + "/invalid.json"

	require.NoError(t, os.WriteFile(cachePath, []byte("invalid json"), 0o644))

	cache := newCache[[]catwalk.Provider](cachePath)

	_, _, err := cache.Get()
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to unmarshal provider data from cache")
}

func TestCachePathFor(t *testing.T) {
	tests := []struct {
		name        string
		xdgDataHome string
		expected    string
	}{
		{
			name:        "with XDG_DATA_HOME",
			xdgDataHome: "/custom/data",
			expected:    "/custom/data/crush/providers.json",
		},
		{
			name:        "without XDG_DATA_HOME",
			xdgDataHome: "",
			expected:    "", // Will use platform-specific default.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.xdgDataHome != "" {
				t.Setenv("XDG_DATA_HOME", tt.xdgDataHome)
			} else {
				t.Setenv("XDG_DATA_HOME", "")
			}

			result := cachePathFor("providers")
			if tt.expected != "" {
				require.Equal(t, tt.expected, filepath.ToSlash(result))
			} else {
				require.Contains(t, result, "crush")
				require.Contains(t, result, "providers.json")
			}
		})
	}
}

// TestProviders_KeepsCatalogWhenCachingFails covers the case that used to
// sign Hyper users out: the provider list was fetched successfully but could
// not be written to the on-disk cache, and Providers discarded it. Hyper's
// endpoint and models live in the catalog rather than in the user's config,
// so losing it there removed the provider entirely and invalidated the
// user's saved model.
func TestProviders_KeepsCatalogWhenCachingFails(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// A file where a directory needs to be, so every cache write fails.
	blocked := filepath.Join(tmpDir, "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("block"), 0o644))
	unwritable := filepath.Join(blocked, "subdir", "cache.json")

	resetProviderState()
	defer resetProviderState()

	// Prime both syncers with mock clients so Providers reuses the memoized
	// outcome instead of reaching the network.
	catwalkSyncer.Init(&mockCatwalkClient{
		providers: []catwalk.Provider{{Name: "Provider1", ID: "p1"}},
	}, unwritable, true)
	hyperSyncer.Init(&mockHyperClient{
		provider: catwalk.Provider{
			Name:   "Hyper",
			ID:     "hyper",
			Models: []catwalk.Model{{ID: "hyper-1", Name: "Hyper Model"}},
		},
	}, unwritable, true)

	catwalkProviders, catwalkErr := catwalkSyncer.Get(t.Context())
	require.Error(t, catwalkErr, "cache write should fail")
	require.NotEmpty(t, catwalkProviders, "syncer still returns a usable catalog")

	hyperProvider, hyperErr := hyperSyncer.Get(t.Context())
	require.Error(t, hyperErr, "cache write should fail")
	require.Equal(t, "Hyper", hyperProvider.Name)

	providers, err := Providers(&Config{Options: &Options{}})

	// The failure is reported, but as a warning alongside a usable catalog.
	require.Error(t, err)
	require.Len(t, providers, 2)
	require.Equal(t, catwalk.InferenceProvider("hyper"), providers[0].ID, "Hyper stays at the front")
	require.Equal(t, catwalk.InferenceProvider("p1"), providers[1].ID)
}

// TestProviders_FallsBackToEmbeddedHyper checks that Hyper is still in the
// catalog when it could not be fetched at all, using the copy bundled with
// this release.
func TestProviders_FallsBackToEmbeddedHyper(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	resetProviderState()
	defer resetProviderState()

	catwalkSyncer.Init(&mockCatwalkClient{
		providers: []catwalk.Provider{{Name: "Provider1", ID: "p1"}},
	}, filepath.Join(tmpDir, "providers.json"), true)
	hyperSyncer.Init(&mockHyperClient{
		err: errors.New("network error"),
	}, filepath.Join(tmpDir, "hyper.json"), true)

	_, _ = catwalkSyncer.Get(t.Context())
	_, _ = hyperSyncer.Get(t.Context())

	providers, err := Providers(&Config{Options: &Options{}})
	require.NoError(t, err)
	require.Len(t, providers, 2)
	require.Equal(t, catwalk.InferenceProvider("hyper"), providers[0].ID)
	require.NotEmpty(t, providers[0].Models, "the embedded Hyper provider carries models")
}

// TestProviders_HonorsDisableDefaultProviders makes sure the embedded Hyper
// fallback does not smuggle a default provider back in.
func TestProviders_HonorsDisableDefaultProviders(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	resetProviderState()
	defer resetProviderState()

	providers, err := Providers(&Config{
		Options: &Options{DisableDefaultProviders: true},
	})
	require.NoError(t, err)
	require.Empty(t, providers)
}

// TestCacheStore_ReplacesFileInsteadOfRewritingIt guards the property that
// several Crush instances depend on: the provider cache is swapped into place
// as a finished file, never truncated and refilled underneath a reader that is
// already reading it. A reader that loses that race cannot parse the catalog
// and silently falls back to the bundled copy.
func TestCacheStore_ReplacesFileInsteadOfRewritingIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	c := newCache[[]catwalk.Provider](path)

	require.NoError(t, c.Store([]catwalk.Provider{{ID: "first", Name: "First"}}))
	before, err := os.Stat(path)
	require.NoError(t, err)

	require.NoError(t, c.Store([]catwalk.Provider{{ID: "second", Name: "Second"}}))
	after, err := os.Stat(path)
	require.NoError(t, err)

	// os.Stat on Windows resolves file identity lazily by reopening the path,
	// so both stats describe whichever file the path points at by the time
	// they are compared and SameFile cannot observe the replacement. The
	// write path is shared, so asserting this on the other platforms covers
	// it. The checks below still run everywhere.
	if runtime.GOOS != "windows" {
		require.False(t, os.SameFile(before, after),
			"the cache should be replaced by a rename, not rewritten in place")
	}

	// The new contents are complete and no temporary files are left behind.
	got, _, err := c.Get()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, catwalk.InferenceProvider("second"), got[0].ID)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the cache file should remain")
	require.Equal(t, "providers.json", entries[0].Name())
}
