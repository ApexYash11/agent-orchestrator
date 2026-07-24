# OpenCode Model Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make OpenCode launch and restore commands honor CLI-configured project models while removing model editing from desktop Project Settings.

**Architecture:** Keep model storage and resolution unchanged. Add the missing model-to-command translation inside the OpenCode adapter, and make the desktop settings form treat model configuration as hidden state that it preserves during replacement saves.

**Tech Stack:** Go, Cobra-backed project configuration, React 19, TypeScript, Vitest, Testing Library.

## Global Constraints

- Model configuration remains available through `ao project set-config <id> --model ...`.
- Blank models do not emit an OpenCode CLI flag.
- The settings form must preserve model values loaded from project config.
- No API, storage, or generated-code changes.
- No startup or missing-model warning.

---

### Task 1: Forward configured models through the OpenCode adapter

**Files:**
- Modify: `backend/internal/adapters/agent/opencode/opencode_test.go`
- Modify: `backend/internal/adapters/agent/opencode/opencode.go`

**Interfaces:**
- Consumes: `ports.AgentConfig.Model string`, `ports.LaunchConfig.Config`, and `ports.RestoreConfig.Config`.
- Produces: `appendModelFlag(cmd *[]string, cfg ports.AgentConfig)` and an OpenCode `GetConfigSpec` model field.

- [ ] **Step 1: Write failing adapter tests**

Replace `TestGetConfigSpecHasNoCustomFieldsYet` with `TestGetConfigSpecReportsModelField`, expecting:

```go
[]ports.ConfigField{{
    Key:         "model",
    Type:        ports.ConfigFieldString,
    Description: "Model override passed to `opencode --model`, in provider/model form.",
}}
```

Add launch tests that expect `ports.AgentConfig{Model: "  anthropic/claude-4.5-sonnet  "}` to produce:

```go
[]string{"opencode", "--model", "anthropic/claude-4.5-sonnet"}
```

and a whitespace-only model to omit `--model`. Add a restore test with native session metadata that expects:

```go
[]string{
    "opencode",
    "--model", "anthropic/claude-4.5-sonnet",
    "--session", "ses_abc123",
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```powershell
Set-Location backend
go test ./internal/adapters/agent/opencode -run 'TestGetConfigSpecReportsModelField|TestGetLaunchCommand(AppendsConfiguredModel|OmitsBlankConfiguredModel)|TestGetRestoreCommandAppendsConfiguredModel'
```

Expected: FAIL because the config spec is empty and launch/restore commands omit `--model`.

- [ ] **Step 3: Implement the minimal adapter behavior**

Add:

```go
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
    if err := ctx.Err(); err != nil {
        return ports.ConfigSpec{}, err
    }
    return ports.ConfigSpec{
        Fields: []ports.ConfigField{{
            Key:         "model",
            Type:        ports.ConfigFieldString,
            Description: "Model override passed to `opencode --model`, in provider/model form.",
        }},
    }, nil
}

func appendModelFlag(cmd *[]string, cfg ports.AgentConfig) {
    if model := strings.TrimSpace(cfg.Model); model != "" {
        *cmd = append(*cmd, "--model", model)
    }
}
```

Call `appendModelFlag(&cmd, cfg.Config)` after permission flags in both `GetLaunchCommand` and `GetRestoreCommand`.

- [ ] **Step 4: Run adapter tests and verify GREEN**

Run:

```powershell
Set-Location backend
go test ./internal/adapters/agent/opencode
```

Expected: PASS.

- [ ] **Step 5: Commit the backend behavior**

```powershell
git add backend/internal/adapters/agent/opencode/opencode.go backend/internal/adapters/agent/opencode/opencode_test.go
git commit -m "fix(opencode): pass configured model to sessions"
```

### Task 2: Remove model editing from Project Settings without losing CLI state

**Files:**
- Modify: `frontend/src/renderer/components/ProjectSettingsForm.test.tsx`
- Modify: `frontend/src/renderer/components/ProjectSettingsForm.tsx`

**Interfaces:**
- Consumes: loaded `ProjectConfig.agentConfig`.
- Produces: a settings replacement payload that preserves `agentConfig.model` and updates only exposed agent settings.

- [ ] **Step 1: Change the settings test to express the desired behavior**

In the existing save test, assert:

```ts
expect(screen.queryByLabelText("Model override")).not.toBeInTheDocument();
```

Remove interactions with that input. Keep the loaded model `claude-opus-4-5` and expect the save payload to contain:

```ts
agentConfig: {
    model: "claude-opus-4-5",
    permissions: "bypass-permissions",
},
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```powershell
Set-Location frontend
npx vitest run src/renderer/components/ProjectSettingsForm.test.tsx
```

Expected: FAIL because the "Model override" input is still rendered.

- [ ] **Step 3: Implement the minimal settings change**

Remove `model` from the form state and delete the `Field` containing the `model` input. Change the replacement payload from:

```ts
agentConfig: blankToUndefined({
    ...config.agentConfig,
    model: form.model || undefined,
    permissions: form.permissions || undefined,
}),
```

to:

```ts
agentConfig: blankToUndefined({
    ...config.agentConfig,
    permissions: form.permissions || undefined,
}),
```

- [ ] **Step 4: Run settings tests and verify GREEN**

Run:

```powershell
Set-Location frontend
npx vitest run src/renderer/components/ProjectSettingsForm.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Run complete touched-area verification**

Run:

```powershell
Set-Location backend
go test ./internal/adapters/agent/opencode
Set-Location ../frontend
npm run typecheck
npm run build
```

Expected: all commands exit 0.

- [ ] **Step 6: Commit the frontend behavior**

```powershell
git add frontend/src/renderer/components/ProjectSettingsForm.tsx frontend/src/renderer/components/ProjectSettingsForm.test.tsx
git commit -m "fix(settings): keep model configuration CLI-only"
```
