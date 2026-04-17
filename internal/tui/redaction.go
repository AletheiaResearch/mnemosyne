package tui

func redactionScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Redaction",
		Body:  "The extraction pipeline applies secret detectors, literal redactions, and stable anonymization to every exported textual field.",
	}
}
