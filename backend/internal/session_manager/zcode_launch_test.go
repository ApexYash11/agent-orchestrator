package sessionmanager

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/zcode"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// TestZCodeRestoreFallbackDeliversInstructionsInline covers a restore whose
// native conversation is gone (ZCode reports no native session id, so every
// restore relaunches fresh): the launch must now succeed — the standing
// instructions are delivered through the session's private instructions home
// instead of failing the launch — and the argv must carry neither the
// instructions nor the saved task.
func TestZCodeRestoreFallbackDeliversInstructionsInline(t *testing.T) {
	binDir := t.TempDir()
	binary := "zcode"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, binary), []byte("test executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	for _, kind := range []domain.SessionKind{domain.KindWorker, domain.KindOrchestrator} {
		for _, source := range []string{"inline", "file"} {
			t.Run(string(kind)+"/"+source, func(t *testing.T) {
				inline, file := "AO role instructions", ""
				dataDir := t.TempDir()
				if source == "file" {
					file = filepath.Join(t.TempDir(), "system.md")
					if err := os.WriteFile(file, []byte(inline), 0o600); err != nil {
						t.Fatal(err)
					}
					inline = ""
				}
				meta := domain.SessionMetadata{AgentSessionID: "sess_previous"}
				if kind == domain.KindWorker {
					meta.Prompt = "saved task"
				}
				argv, delivery, mode, err := restoreArgv(t.Context(), zcode.New(), "ao-1", t.TempDir(), meta,
					inline, file, ports.AgentConfig{}, kind, domain.HarnessZCode, dataDir, nil)
				if err != nil {
					t.Fatalf("restore = %v; want a fresh launch with private instruction delivery", err)
				}
				if delivery != ports.PromptDeliveryAfterStart {
					t.Fatalf("delivery = %v, want AfterStart", delivery)
				}
				if kind == domain.KindWorker && mode != RestoreModeSavedPrompt {
					t.Fatalf("mode = %v, want SavedPrompt for a worker with a saved task", mode)
				}
				if len(argv) == 0 {
					t.Fatal("restore produced no argv")
				}
				for _, arg := range argv {
					if strings.Contains(arg, "AO role instructions") || strings.Contains(arg, "saved task") {
						t.Fatalf("argv %v carried instructions or the task", argv)
					}
				}
				delivered, err := os.ReadFile(filepath.Join(dataDir, "zcode", "instruction-homes", "ao-1", ".zcode", "AGENTS.md"))
				if err != nil {
					t.Fatalf("instructions home not written: %v", err)
				}
				if !strings.Contains(string(delivered), "AO role instructions") {
					t.Fatalf("AGENTS.md = %q; want the standing instructions on the private channel", delivered)
				}
			})
		}
	}
}

// TestZCodeRestoreComposedPromptCarriesOnlyTheTask pins the composed first
// message: the task alone. Standing instructions travel through the private
// instructions home, so the typed message does not echo AO's role text back
// into a conversation the user may watch.
func TestZCodeRestoreComposedPromptCarriesOnlyTheTask(t *testing.T) {
	composed, err := zcode.New().BuildAfterStartPrompt(t.Context(), ports.LaunchConfig{
		SystemPrompt: "AO role instructions",
		Prompt:       "saved task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if composed != "saved task" {
		t.Fatalf("composed = %q; want the bare task with nothing typed back", composed)
	}
}
