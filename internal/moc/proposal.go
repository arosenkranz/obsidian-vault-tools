// internal/moc/proposal.go
package moc

// Proposal matches moc_cleanup.py's JSON response shape exactly
// (behavior inventory row #112): new_content is required; duplicates_
// flagged and summary default to Go's zero values (nil slice / empty
// string) when absent from the LLM's response, mirroring v1's dict.
// setdefault.
type Proposal struct {
	NewContent        string   `json:"new_content"`
	DuplicatesFlagged []string `json:"duplicates_flagged"`
	Summary           string   `json:"summary"`
}
