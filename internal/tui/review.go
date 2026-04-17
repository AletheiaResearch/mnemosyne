package tui

func reviewScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Review and Attest",
		Body:  "Attestation requires three substantive free-form statements: identity scan, sensitive-entity interview, and manual sample review.\n\nTypical command:\n  mnemosyne attest --full-name \"...\" --identity-scan \"...\" --entity-scan \"...\" --manual-review \"...\"",
	}
}
