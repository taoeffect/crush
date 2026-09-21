package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// sidRecordingCoordinator reports a queued message and records the
// session ID the pop route handed it, so the test can tell a bound
// {sid} from an empty one.
type sidRecordingCoordinator struct {
	*runCoordinator
	poppedSession atomic.Value
}

func (s *sidRecordingCoordinator) PopQueuedMessage(sessionID string) (agent.QueuedMessage, bool) {
	s.poppedSession.Store(sessionID)
	return agent.QueuedMessage{Prompt: "queued"}, true
}

// routedWorkspace serves one synthetic workspace through the
// production handler chain, so requests go through the same mux
// NewServer builds instead of calling handlers directly.
func routedWorkspace(t *testing.T, coord agent.Coordinator) (*httptest.Server, string) {
	t.Helper()
	cfg := config.NewTestStore(&config.Config{})
	b := backend.New(context.Background(), cfg, nil)
	ws := &backend.Workspace{
		ID:   uuid.New().String(),
		Path: t.TempDir(),
		App:  &app.App{AgentCoordinator: coord},
		Cfg:  cfg,
	}
	backend.InsertWorkspaceForTest(b, ws)
	backend.SetWorkspaceShutdownFnForTest(ws, func() {})

	srv := &Server{backend: b}
	srv.installHandler()
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs, ws.ID
}

// TestRoutesReachTheirHandlers covers the routes this fork adds to the
// endpoint registry. Every other test for them binds path values by
// hand and calls the handler directly, so a route that never made it
// into the registry — or one whose pattern names different path
// parameters than its handler reads — would still look fully covered
// while every client got a 404.
func TestRoutesReachTheirHandlers(t *testing.T) {
	t.Parallel()

	coord := &sidRecordingCoordinator{
		runCoordinator: newRunCoordinator(func(context.Context) error { return nil }),
	}
	hs, wsID := routedWorkspace(t, coord)

	do := func(t *testing.T, method, path string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, hs.URL+path, nil)
		require.NoError(t, err)
		resp, err := hs.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		// A mux miss is a 404 with a plain-text body, so each case
		// asserts a status its handler alone produces.
		return resp.StatusCode, body
	}

	t.Run("pop queued prompt", func(t *testing.T) {
		status, body := do(t, http.MethodPost,
			"/v1/workspaces/"+wsID+"/agent/sessions/S1/prompts/pop")
		require.Equal(t, http.StatusOK, status)

		var got proto.PopQueuedMessageResponse
		require.NoError(t, json.Unmarshal(body, &got))
		require.True(t, got.Found)
		require.Equal(t, "queued", got.Message.Prompt)
		require.Equal(t, "S1", coord.poppedSession.Load(),
			"the session in the path must reach the coordinator")
	})

	t.Run("cancel run", func(t *testing.T) {
		// Cancelling a run the server never registered is a success,
		// so a 200 here is the handler's answer and not the mux's.
		status, _ := do(t, http.MethodPost,
			"/v1/workspaces/"+wsID+"/agent/runs/"+uuid.New().String()+"/cancel")
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("has config field", func(t *testing.T) {
		status, body := do(t, http.MethodGet,
			"/v1/workspaces/"+wsID+"/config/has?scope=global&key=models.large")
		require.Equal(t, http.StatusOK, status)

		var got proto.ConfigHasFieldResponse
		require.NoError(t, json.Unmarshal(body, &got))
		require.False(t, got.Exists)
	})

	t.Run("save model choices as default", func(t *testing.T) {
		status, body := do(t, http.MethodPost,
			"/v1/workspaces/"+wsID+"/config/model/default")
		require.Equal(t, http.StatusBadRequest, status)

		var got proto.Error
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, proto.ErrCodeNoModelChoicesToSave, got.Code,
			"the route must keep the error code clients branch on")
	})
}
