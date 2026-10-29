package sessionmanager

import (
	"os"
	"path/filepath"
	"reflect"
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
// instructions ride the first typed message via BuildAfterStartPrompt instead
// of failing the launch — and the argv must carry neither the instructions nor
// the saved task.
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
					inline, file, ports.AgentConfig{}, kind, domain.HarnessZCode, t.TempDir(), nil)
				if err != nil {
					t.Fatalf("restore = %v; want a fresh launch with inline instruction delivery", err)
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
			})
		}
	}
}

// TestZCodeRestoreComposedPromptCarriesInstructionsAndTask pins the composed
// first message: framed instructions ahead of the saved task, matching what
// the messenger types after the TUI is ready.
func TestZCodeRestoreComposedPromptCarriesInstructionsAndTask(t *testing.T) {
	composed, err := zcode.New().BuildAfterStartPrompt(t.Context(), ports.LaunchConfig{
		SystemPrompt: "AO role instructions",
		Prompt:       "saved task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(composed, "AO role instructions") || !strings.Contains(composed, "saved task") {
		t.Fatalf("composed = %q; want instructions and task", composed)
	}
	if strings.Index(composed, "AO role instructions") > strings.Index(composed, "saved task") {
		t.Fatalf("composed = %q; want instructions before the task", composed)
	}
	if !reflect.DeepEqual(strings.Count(composed, "--- Task ---"), 1) {
		t.Fatalf("composed = %q; want exactly one task marker", composed)
	}
}
