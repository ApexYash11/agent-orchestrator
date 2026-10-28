package sessionmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/zcode"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestZCodeRestoreFallbackRejectsStandingInstructions(t *testing.T) {
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
				argv, _, _, err := restoreArgv(t.Context(), zcode.New(), "ao-1", t.TempDir(), meta,
					inline, file, ports.AgentConfig{}, kind, domain.HarnessZCode, t.TempDir(), nil)
				if argv != nil || err == nil || !strings.Contains(err.Error(), "AO standing instructions") {
					t.Fatalf("restore = %v, %v; want instruction rejection before any fresh launch", argv, err)
				}
			})
		}
	}
}
