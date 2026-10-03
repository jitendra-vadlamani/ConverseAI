package agent

import (
	_ "embed"
	"strings"
	"time"
)

// PromptVersion is recorded on every run so answers can be traced back to
// the exact prompt that produced them. Bump it whenever a prompt file changes.
const PromptVersion = "system.v1+tools.v1"

//go:embed prompts/system.v1.md
var systemPrompt string

//go:embed prompts/tools.v1.md
var toolsPrompt string

// SystemPrompt renders the system prompt; withTools adds the tool guidance.
func SystemPrompt(now time.Time, withTools bool) string {
	p := strings.ReplaceAll(systemPrompt, "{{date}}", now.Format("Monday, 2 January 2006"))
	if withTools {
		p += "\n" + toolsPrompt
	}
	return p
}
