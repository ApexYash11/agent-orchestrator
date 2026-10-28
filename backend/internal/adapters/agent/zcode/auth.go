package zcode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// zcodeAuthEnvNames are the API-key environments ZCode's CLI and provider
// configuration accept for key-based access. Z.ai's GLM Coding Plan login
// stores its credential in the CLI's own credential store, which has no
// documented read-only probe.
var zcodeAuthEnvNames = []string{"ZAI_API_KEY", "ZCODE_API_KEY", "GLM_API_KEY", "Z_AI_API_KEY"}

// AuthStatus reports the selected local auth route from documented credential
// locations only. ZCode's `zcode doctor --json` output schema is not documented
// in the open-source usage guide, so AO does not parse it; instead a
// non-empty API key in the environment or the personal provider configuration
// reports configured, and anything else reports unknown (account logins live in
// an opaque credential store AO cannot read). Configured therefore means
// "present", not "verified against the provider".
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	if err := ctx.Err(); err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	for _, name := range zcodeAuthEnvNames {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return ports.AgentAuthStatusConfigured, nil
		}
	}
	data, err := os.ReadFile(zcodeProviderConfigPath())
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return ports.AgentAuthStatusUnknown, nil
	}
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	if providerConfigHasAPIKey(data) {
		return ports.AgentAuthStatusConfigured, nil
	}
	return ports.AgentAuthStatusUnknown, nil
}

// zcodeProviderConfigPath resolves the personal model configuration file. See
// docs/usage/03-config.md in zai-org/ZCode: ZCODE_PERSONAL_PROVIDER_CONFIG_FILE
// overrides the file, ZCODE_DATA_BASE_DIR moves the data root, and the default
// is ~/.zcode/v2/provider_config.json.
func zcodeProviderConfigPath() string {
	if file := strings.TrimSpace(os.Getenv("ZCODE_PERSONAL_PROVIDER_CONFIG_FILE")); file != "" {
		return file
	}
	base := strings.TrimSpace(os.Getenv("ZCODE_DATA_BASE_DIR"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".zcode")
	}
	return filepath.Join(base, "v2", "provider_config.json")
}

// providerConfigHasAPIKey walks the decoded configuration for any non-empty
// apiKey value. The file's schema is versioned by its author (`schemaVersion`),
// so AO matches on the field name rather than a fixed path.
func providerConfigHasAPIKey(data []byte) bool {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}
	return walkAPIKey(doc)
}

func walkAPIKey(node any) bool {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if strings.EqualFold(strings.TrimSpace(key), "apiKey") {
				if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
					return true
				}
			}
			if walkAPIKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if walkAPIKey(child) {
				return true
			}
		}
	}
	return false
}
