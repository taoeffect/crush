package update

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckForUpdate_Old(t *testing.T) {
	info, err := Check(t.Context(), "v0.10.0", testClient{"v0.11.0"})
	require.NoError(t, err)
	require.NotNil(t, info)
	require.True(t, info.Available())
}

func TestCheckForUpdate_Beta(t *testing.T) {
	t.Run("current is stable", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.10.0", testClient{"v0.11.0-beta.1"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.False(t, info.Available())
	})

	t.Run("current is also beta", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.11.0-beta.1", testClient{"v0.11.0-beta.2"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.True(t, info.Available())
	})

	t.Run("current is beta, latest isn't", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.11.0-beta.1", testClient{"v0.11.0"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.True(t, info.Available())
	})
}

func TestCheckForUpdate_TaoEffect(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		latest    string
		available bool
	}{
		{
			name:      "fork version matches upstream base version",
			current:   "v0.83.0-taoeffect.1",
			latest:    "v0.83.0",
			available: false,
		},
		{
			name:      "upstream version matches fork base version",
			current:   "v0.83.0",
			latest:    "v0.83.0-taoeffect.1",
			available: false,
		},
		{
			name:      "newer fork version with same upstream base",
			current:   "v0.83.0-taoeffect.1",
			latest:    "v0.83.0-taoeffect.2",
			available: true,
		},
		{
			name:      "newer upstream base version",
			current:   "v0.83.0-taoeffect.1",
			latest:    "v0.84.0",
			available: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := Check(t.Context(), tt.current, testClient{tt.latest})
			require.NoError(t, err)
			require.NotNil(t, info)
			require.Equal(t, tt.available, info.Available())
		})
	}
}

type testClient struct{ tag string }

// Latest implements Client.
func (t testClient) Latest(ctx context.Context) (*Release, error) {
	return &Release{
		TagName: t.tag,
		HTMLURL: "https://example.org",
	}, nil
}
