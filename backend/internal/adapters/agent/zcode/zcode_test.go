package zcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestManifest(t *testing.T) {
	m := (&Plugin{}).Manifest()
	if m.ID != "zcode" {
		t.Fatalf("ID = %q, want zcode", m.ID)
	}
	if m.Name != "ZCode" {
		t.Fatalf("Name = %q, want ZCode", m.Name)
	}
	hasAgent := false
	for _, c := range m.Capabilities {
		if c == adapters.CapabilityAgent {
			hasAgent = true
		}
	}
	if !hasAgent {
		t.Fatal("missing CapabilityAgent")
	}
}

func TestGetPromptDeliveryStrategyIsAfterStart(t *testing.T) {
	strategy, err := (&Plugin{}).GetPromptDeliveryStrategy(context.Background(), ports.LaunchConfig{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strategy != ports.PromptDeliveryAfterStart {
		t.Fatalf("strategy = %q, want %q", strategy, ports.PromptDeliveryAfterStart)
	}
}

func TestGetConfigSpecReportsModes(t *testing.T) {
	spec, err := (&Plugin{}).GetConfigSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Fields) != 1 || spec.Fields[0].Key != "mode" {
		t.Fatalf("unexpected fields: %#v", spec.Fields)
	}
	want := []string{"plan", "build", "edit", "yolo", "guarded"}
	if !reflect.DeepEqual(spec.Fields[0].Enum, want) {
		t.Fatalf("mode enum = %#v, want %#v", spec.Fields[0].Enum, want)
	}
}

func TestGetLaunchCommandPinsConfiguredMode(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "zcode"}
	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions: ports.PermissionModeAuto,
		Config:      ports.AgentConfig{Mode: "plan"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"zcode", "--mode", "plan"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("cmd = %#v, want %#v (explicit session mode wins over permissions)", cmd, want)
	}
}

func TestGetLaunchCommandRejectsUnknownMode(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "zcode"}
	if _, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{Mode: "auto"},
	}); err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("err = %v, want unknown-mode rejection", err)
	}
}

func TestGetLaunchCommandDeliversNoPrompt(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "zcode"}

	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Prompt: "add a health check",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"zcode"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("cmd = %#v, want %#v", cmd, want)
	}
	for _, arg := range cmd {
		if arg == "--prompt" || arg == "-p" || arg == "add a health check" {
			t.Fatalf("cmd = %#v unexpectedly carries the task in argv", cmd)
		}
	}
}

func TestGetLaunchCommandRejectsStandingInstructions(t *testing.T) {
	const privateInstructions = "PRIVATE AO worker role instructions"
	for _, test := range []struct {
		name     string
		inline   string
		file     string
		withFile bool
	}{
		{name: "inline", inline: privateInstructions},
		{name: "file only", file: privateInstructions, withFile: true},
		{name: "blank inline with file", inline: " \n\t", file: privateInstructions, withFile: true},
		{name: "inline with empty file", inline: privateInstructions, withFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := ports.LaunchConfig{SystemPrompt: test.inline}
			if test.withFile {
				cfg.SystemPromptFile = filepath.Join(t.TempDir(), "system.md")
				if err := os.WriteFile(cfg.SystemPromptFile, []byte(test.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// No binary cache: unsupported instructions must fail before resolution.
			cmd, err := New().GetLaunchCommand(t.Context(), cfg)
			if cmd != nil || err == nil || !strings.Contains(err.Error(), "AO standing instructions") || !strings.Contains(err.Error(), "choose another harness") {
				t.Fatalf("launch = %v, %v; want no argv and an actionable instruction error", cmd, err)
			}
			if strings.Contains(err.Error(), privateInstructions) {
				t.Fatal("launch error exposed private instructions")
			}
		})
	}
}

func TestGetLaunchCommandAllowsEmptyStandingInstructions(t *testing.T) {
	for _, test := range []struct {
		name     string
		inline   string
		file     string
		withFile bool
	}{
		{name: "absent"},
		{name: "blank inline", inline: " \n\t"},
		{name: "empty file", withFile: true},
		{name: "blank file", inline: " \n", file: " \n\t", withFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := ports.LaunchConfig{SystemPrompt: test.inline, Prompt: "user task", Permissions: ports.PermissionModeAcceptEdits}
			if test.withFile {
				cfg.SystemPromptFile = filepath.Join(t.TempDir(), "system.md")
				if err := os.WriteFile(cfg.SystemPromptFile, []byte(test.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd, err := (&Plugin{resolvedBinary: "zcode"}).GetLaunchCommand(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"zcode", "--mode", "edit"}; !reflect.DeepEqual(cmd, want) {
				t.Fatalf("argv = %v, want %v", cmd, want)
			}
		})
	}
}

func TestGetLaunchCommandRejectsUnreadableInstructionFile(t *testing.T) {
	cmd, err := New().GetLaunchCommand(t.Context(), ports.LaunchConfig{
		SystemPromptFile: filepath.Join(t.TempDir(), "missing.md"),
	})
	if cmd != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("launch = %v, %v; want no argv and the instruction-file read error", cmd, err)
	}
}

func TestGetLaunchCommandHonorsCancellationBeforeInstructions(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd, err := New().GetLaunchCommand(ctx, ports.LaunchConfig{SystemPrompt: "AO instructions"})
	if cmd != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("launch = %v, %v; want no argv and context.Canceled", cmd, err)
	}
}

func TestGetLaunchCommandMapsPermissionModes(t *testing.T) {
	tests := []struct {
		name string
		mode ports.PermissionMode
		want []string
	}{
		{name: "default keeps zcode's own mode", mode: ports.PermissionModeDefault, want: []string{"zcode"}},
		{name: "empty keeps zcode's own mode", mode: "", want: []string{"zcode"}},
		{name: "accept edits", mode: ports.PermissionModeAcceptEdits, want: []string{"zcode", "--mode", "edit"}},
		{name: "auto", mode: ports.PermissionModeAuto, want: []string{"zcode", "--mode", "yolo"}},
		{name: "bypass permissions", mode: ports.PermissionModeBypassPermissions, want: []string{"zcode", "--mode", "yolo"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plugin := &Plugin{resolvedBinary: "zcode"}
			cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{Permissions: test.mode})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cmd, test.want) {
				t.Fatalf("cmd = %#v, want %#v", cmd, test.want)
			}
		})
	}
}

func TestAdapterDeclaresNoRestoreOrSessionInfo(t *testing.T) {
	plugin := &Plugin{}
	cmd, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Session: ports.SessionRef{Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "sess_123"}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok || cmd != nil {
		t.Fatalf("restore = %#v ok=%t, want no restore until a trusted session id source exists", cmd, ok)
	}
	if _, ok, err := plugin.SessionInfo(context.Background(), ports.SessionRef{}); err != nil || ok {
		t.Fatalf("SessionInfo ok=%t err=%v, want no metadata", ok, err)
	}
}

func TestAdapterReportsNoTerminalVersionRefresh(t *testing.T) {
	capability, ok := any(&Plugin{}).(ports.AgentTerminalVersionRefresher)
	if !ok {
		t.Fatal("zcode must declare its terminal version refresh capability")
	}
	if capability.SupportsTerminalVersionRefresh() {
		t.Fatal("zcode has no trusted native id source, so it cannot refresh in place")
	}
}

func TestResolveBinaryReportsMissingInstall(t *testing.T) {
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	if _, err := ResolveZCodeBinary(context.Background()); !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("err = %v, want ErrAgentBinaryNotFound", err)
	}
}

func TestValidNativeSessionID(t *testing.T) {
	if !validNativeSessionID("sess_4e06c751-4bc7-4aca-8d82-0ac6d00e924d") {
		t.Fatal("released zcode session id rejected")
	}
	for _, id := range []string{"", "sess_", "4e06c751-4bc7"} {
		if validNativeSessionID(id) {
			t.Fatalf("id %q accepted", id)
		}
	}
}

func TestAuthStatusReadsDocumentedCredentialLocations(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		providerConf string
		want         ports.AgentAuthStatus
	}{
		{
			name: "no credentials anywhere",
			want: ports.AgentAuthStatusUnknown,
		},
		{
			name: "environment key",
			env:  map[string]string{"ZAI_API_KEY": "key"},
			want: ports.AgentAuthStatusConfigured,
		},
		{
			name:         "provider config key",
			providerConf: `{"schemaVersion":1,"config":{"providerConfigRules":{"providerRules":[{"config":{"access":{"type":"api-key","apiKey":"key"}}}]}}}`,
			want:         ports.AgentAuthStatusConfigured,
		},
		{
			name:         "provider config without a key stays unknown (account logins are opaque)",
			providerConf: `{"schemaVersion":1,"config":{"providerConfigRules":{"providerRules":[]}}}`,
			want:         ports.AgentAuthStatusUnknown,
		},
		{
			name:         "blank key does not count",
			providerConf: `{"config":{"providerConfigRules":{"providerRules":[{"config":{"access":{"apiKey":"  "}}}]}}}`,
			want:         ports.AgentAuthStatusUnknown,
		},
		{
			name:         "invalid json stays unknown",
			providerConf: `{`,
			want:         ports.AgentAuthStatusUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ZAI_API_KEY", "")
			t.Setenv("ZCODE_API_KEY", "")
			t.Setenv("GLM_API_KEY", "")
			t.Setenv("Z_AI_API_KEY", "")
			t.Setenv("ZCODE_DATA_BASE_DIR", t.TempDir())
			config := ""
			if test.providerConf != "" {
				config = filepath.Join(t.TempDir(), "provider_config.json")
				if err := os.WriteFile(config, []byte(test.providerConf), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE", config)
			for name, value := range test.env {
				t.Setenv(name, value)
			}

			got, err := (&Plugin{}).AuthStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("AuthStatus = %q, want %q", got, test.want)
			}
		})
	}
}

func TestProviderConfigPathPrefersOverrides(t *testing.T) {
	t.Setenv("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE", "custom.json")
	if got := zcodeProviderConfigPath(); got != "custom.json" {
		t.Fatalf("path = %q, want custom.json", got)
	}
	t.Setenv("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE", "")
	t.Setenv("ZCODE_DATA_BASE_DIR", filepath.Join("base", "dir"))
	want := filepath.Join("base", "dir", "v2", "provider_config.json")
	if got := zcodeProviderConfigPath(); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
