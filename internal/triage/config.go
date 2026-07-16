// internal/triage/config.go
package triage

// Config is the narrow subset of ov config the triage package needs —
// kept separate from internal/config.Config (design spec's "stateless
// verbs" principle, same pattern as capture.CaptureConfig / llm.Config).
type Config struct {
	VaultDir  string
	Inbox     string
	Projects  string
	Areas     string
	Resources string
	Archive   string
}

// ParaRoots returns the four configured PARA root folder names.
func (c Config) ParaRoots() []string {
	return []string{c.Projects, c.Areas, c.Resources, c.Archive}
}
