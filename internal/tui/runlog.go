package tui

func runlogScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Run Log",
		Body:  "Runlog shows the persisted workflow state: last extract, attestation details, and the latest publication attestation.",
	}
}
