package kiro

import (
	"regexp"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var kiroTerminalEscape = regexp.MustCompile(`\x1b(?:\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]|\][^\x07]*(?:\x07|\x1b\\))`)

// DeriveActivityState maps a Kiro hook sub-command onto an AO activity state.
// The bool is false when the event carries no activity signal.
//
// PreToolUse arrives as pre-tool-use (active): it fires for every tool
// invocation including auto-approved/trusted tools, so it proves work, not a
// pending decision. The legacy permission-request sub-command (agent files
// written before the remap) maps to active for the same reason: no other Kiro
// event emits it, and the old mapping pinned every tool invocation to sticky
// waiting_input. Genuine approval dialogs carry no hook of their own and are
// observed via DetectTerminalActivity instead.
func DeriveActivityState(event string, _ []byte) (domain.ActivityState, bool) {
	switch event {
	case "session-start", "user-prompt-submit":
		return domain.ActivityActive, true
	case "pre-tool-use", "post-tool-use", "post-tool-use-failure", "permission-resolved":
		return domain.ActivityActive, true
	case "permission-request":
		return domain.ActivityActive, true
	case "stop":
		return domain.ActivityIdle, true
	default:
		return "", false
	}
}

// ContinuouslyDetectTerminalActivity opts Kiro into terminal reconciliation
// on every observer tick. Completion hooks provide the primary transition,
// but the TUI approval bar is the only observation path for a genuinely
// pending permission decision.
func (p *Plugin) ContinuouslyDetectTerminalActivity() bool { return true }

// DetectTerminalActivity recognizes authoritative current-state markers in
// Kiro's TUI. The newest marker wins so retained transcript text cannot
// override the live approval bar or generation indicator.
//
//   - Approval: TUI notification/snack bar above the input with Yes / Trust /
//     No options ("requires approval", "yes, single permission", "trust,
//     always allow", classic "allow this action? [y/n/t]"). Kiro fires no
//     hook while the dialog is on screen, so the terminal is the only
//     observation path for a genuinely pending decision.
//   - Active: busy/generation indicator (esc to interrupt/cancel).
//   - Idle: empty composer placeholder ("ask a question or describe a task").
//
// The approval check requires both sides of the dialog affordance (an approve
// marker and a deny marker within a small window), not a lone "trust" or
// "approval" word, so transcript mentions never match.
func (p *Plugin) DetectTerminalActivity(output string) (domain.ActivityState, bool) {
	lines := kiroTerminalLines(output)
	if len(lines) == 0 {
		return "", false
	}
	start := len(lines) - 30
	if start < 0 {
		start = 0
	}
	recent := lines[start:]

	for i := len(recent) - 1; i >= 0; i-- {
		line := strings.ToLower(recent[i])
		if kiroApprovalAt(recent, i) {
			return domain.ActivityWaitingInput, true
		}
		if strings.Contains(line, "esc to interrupt") || strings.Contains(line, "esc to cancel") {
			return domain.ActivityActive, true
		}
		if strings.Contains(line, "ask a question or describe a task") {
			return domain.ActivityIdle, true
		}
	}
	return "", false
}

// kiroApprovalAt reports whether the line at idx is part of a live approval
// dialog: an approval header/prompt anchored with both an approve affordance
// and a deny affordance nearby.
func kiroApprovalAt(lines []string, idx int) bool {
	line := strings.ToLower(lines[idx])
	header := strings.Contains(line, "requires approval") ||
		strings.Contains(line, "awaiting approval") ||
		strings.Contains(line, "allow this action") ||
		strings.Contains(line, "approve tool call") ||
		strings.Contains(line, "yes, single permission") ||
		strings.Contains(line, "notification bar") ||
		strings.Contains(line, "snack bar")
	anchor := header ||
		strings.Contains(line, "[y/n/t]") ||
		strings.Contains(line, "[y]") ||
		strings.Contains(line, "[n]") ||
		strings.Contains(line, "approv") ||
		strings.Contains(line, "trust") ||
		strings.Contains(line, "deny")
	if !anchor {
		return false
	}
	lo := idx - 3
	if lo < 0 {
		lo = 0
	}
	hi := idx + 3
	if hi >= len(lines) {
		hi = len(lines) - 1
	}
	var approve, deny bool
	for i := lo; i <= hi; i++ {
		nearby := strings.ToLower(lines[i])
		if strings.Contains(nearby, "approv") || strings.Contains(nearby, "allow this action") ||
			strings.Contains(nearby, "yes") || strings.Contains(nearby, "trust") ||
			strings.Contains(nearby, "[y]") || strings.Contains(nearby, "y/n") {
			approve = true
		}
		if strings.Contains(nearby, "deny") || strings.Contains(nearby, " no") ||
			strings.HasPrefix(strings.TrimSpace(nearby), "no") ||
			strings.Contains(nearby, "[n]") || strings.Contains(nearby, "y/n") {
			deny = true
		}
	}
	// Only a full approve+deny pair counts: a bare "trust" line without a
	// decision pair (e.g. "--trust-all-tools" help text) must not read as a
	// live dialog.
	return approve && deny
}

func kiroTerminalLines(output string) []string {
	plain := kiroTerminalEscape.ReplaceAllString(strings.ReplaceAll(output, "\r", "\n"), "")
	raw := strings.Split(plain, "\n")
	lines := raw[:0]
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
