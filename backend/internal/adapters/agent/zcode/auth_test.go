package zcode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestAuthProbeRequiresSuccessfulProviderResult(t *testing.T) {
	const success = `{"sessionId":"sess_abc","response":"ok","projection":{"status":"idle","turnCount":1}}`
	cases := []struct {
		name string
		output string
		runErr error
		want ports.AgentAuthStatus
	}{
		{name: "completed summary", output: success, want: ports.AgentAuthStatusAuthorized},
		{name: "metadata only", output: `{"sessionId":"sess_abc"}`, want: ports.AgentAuthStatusUnknown},
		{name: "prompt echo", output: `{"prompt":"Reply with exactly: ok"}`, want: ports.AgentAuthStatusUnknown},
		{name: "metadata and rejection", output: `{"sessionId":"sess_abc","error":"invalid_api_key"}`, want: ports.AgentAuthStatusUnauthorized},
		{name: "echo and rejection", output: `{"prompt":"Reply with exactly: ok","error":"unauthorized"}`, want: ports.AgentAuthStatusUnauthorized},
		{name: "success followed by rejection", output: success + "\ninvalid_api_key", want: ports.AgentAuthStatusUnauthorized},
		{name: "success and process failure", output: success, runErr: errors.New("exit status 1"), want: ports.AgentAuthStatusUnknown},
		{name: "unknown provider error", output: `{"sessionId":"sess_abc","response":"ok","error":"provider unavailable","projection":{"status":"idle","turnCount":1}}`, want: ports.AgentAuthStatusUnknown},
		{name: "incomplete turn", output: `{"sessionId":"sess_abc","response":"ok","projection":{"status":"running","turnCount":1}}`, want: ports.AgentAuthStatusUnknown},
		{name: "no completed turn", output: `{"sessionId":"sess_abc","response":"ok","projection":{"status":"idle","turnCount":0}}`, want: ports.AgentAuthStatusUnknown},
		{name: "bare completion marker", output: `{"stream_completed":true}`, want: ports.AgentAuthStatusUnknown},
		{name: "malformed summary", output: "{", want: ports.AgentAuthStatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := classifyZCodeAuthProbe(tc.output, tc.runErr)
			if got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAuthProbeUsesConfiguredAODataDirectoryAndCleansWorkspace(t *testing.T) {
	installFakeZcode(t, `{"sessionId":"sess_abc","response":"ok","projection":{"status":"idle","turnCount":1}}`)
	dataDir := t.TempDir()
	t.Setenv("AO_DATA_DIR", dataDir)
	status, err := New().AuthStatus(t.Context())
	if err != nil || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("AuthStatus = %q, %v", status, err)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "auth-probes"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("probe workspaces after completion = %v, %v; want empty AO data subdirectory", entries, err)
	}
}

func TestAuthProbeDefaultDirectoryIsUnderAOHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AO_DATA_DIR", "")
	workspace, err := newAuthProbeWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	if filepath.Dir(workspace) != filepath.Join(home, ".ao", "data", "auth-probes") {
		t.Fatalf("workspace = %q; want AO home data root", workspace)
	}
}
