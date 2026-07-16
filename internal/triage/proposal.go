// internal/triage/proposal.go
package triage

// Proposal matches AGENTS.md §7 exactly. JSON tags are the wire contract
// the LLM prompt asks for and internal/llm.ExtractJSON decodes into.
type Proposal struct {
	From             string         `json:"from"`
	To               string         `json:"to"`
	NewTitle         string         `json:"new_title"`
	FrontmatterPatch map[string]any `json:"frontmatter_patch"`
	// BodyPatch is a pointer so JSON `null` and an absent key both decode
	// to nil — Validate rejects any non-nil value regardless of which
	// form the LLM used (row #5).
	BodyPatch  *string  `json:"body_patch"`
	LinksToAdd []string `json:"links_to_add"`
	Rationale  string   `json:"rationale"`
	Confidence string   `json:"confidence"`
}
