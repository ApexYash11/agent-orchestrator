package kiro

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestDeriveActivityStateTreatsPreToolUseAsActive(t *testing.T) {
	for _, event := range []string{"pre-tool-use", "permission-request", "post-tool-use", "permission-resolved"} {
		got, ok := DeriveActivityState(event, []byte(`{}`))
		if got != domain.ActivityActive || !ok {
			t.Fatalf("DeriveActivityState(%q) = (%q, %v), want (%q, true)", event, got, ok, domain.ActivityActive)
		}
	}
}

func TestKiroPreToolUseHookMapsToActiveSignal(t *testing.T) {
	for _, spec := range kiroManagedHooks {
		if spec.Event == "preToolUse" && spec.Command != kiroHookCommandPrefix+"pre-tool-use" {
			t.Fatalf("preToolUse command = %q, want %q", spec.Command, kiroHookCommandPrefix+"pre-tool-use")
		}
	}
}

func TestDetectTerminalActivityReportsApproval(t *testing.T) {
	output := "shell requires approval\n❯ Yes, single permission\n  Trust, always allow in this session\n  No\n"
	got, ok := (&Plugin{}).DetectTerminalActivity(output)
	if got != domain.ActivityWaitingInput || !ok {
		t.Fatalf("DetectTerminalActivity(approval) = (%q, %v), want (%q, true)", got, ok, domain.ActivityWaitingInput)
	}
}

func TestDetectTerminalActivityReportsClassicApproval(t *testing.T) {
	output := "Allow this action? Use 't' to trust (always allow) this tool for the session. [y/n/t]:"
	got, ok := (&Plugin{}).DetectTerminalActivity(output)
	if got != domain.ActivityWaitingInput || !ok {
		t.Fatalf("DetectTerminalActivity(classic) = (%q, %v), want (%q, true)", got, ok, domain.ActivityWaitingInput)
	}
}

func TestDetectTerminalActivityRejectsTrustFlagText(t *testing.T) {
	output := "kiro-cli chat --trust-all-tools\nAllows the model to use any tool without confirmation\n"
	if got, ok := (&Plugin{}).DetectTerminalActivity(output); ok {
		t.Fatalf("DetectTerminalActivity(trust help) = (%q, true), want no signal", got)
	}
}

func TestDetectTerminalActivityApprovalNewestMarkerWins(t *testing.T) {
	approval := "shell requires approval\n❯ Yes, single permission\n  Trust, always allow in this session\n  No\n"
	idle := "ask a question or describe a task\n"
	if got, ok := (&Plugin{}).DetectTerminalActivity(idle + "\n" + approval); got != domain.ActivityWaitingInput || !ok {
		t.Fatalf("approval after idle = (%q, %v), want waiting_input", got, ok)
	}
	if got, ok := (&Plugin{}).DetectTerminalActivity(approval + "\n" + idle); got != domain.ActivityIdle || !ok {
		t.Fatalf("idle after approval = (%q, %v), want idle", got, ok)
	}
}

func TestRemoveKiroLegacyPreToolUse(t *testing.T) {
	entries := []kiroHookEntry{
		{Command: kiroHookCommandPrefix + "permission-request"},
		{Command: "custom hook"},
	}
	kept := removeKiroLegacyPreToolUse(entries, "preToolUse")
	if len(kept) != 1 || kept[0].Command != "custom hook" {
		t.Fatalf("kept = %#v, want only custom hook", kept)
	}
	if got := removeKiroLegacyPreToolUse(entries, "stop"); len(got) != 2 {
		t.Fatalf("non-preToolUse event stripped entries: %#v", got)
	}
}
