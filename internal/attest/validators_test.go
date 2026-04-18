package attest

import "testing"

func TestValidateStatements(t *testing.T) {
	t.Parallel()

	err := ValidateStatements(
		"Alice Example",
		false,
		`I asked for Alice Example and scanned the export for Alice Example before review.`,
		`I asked about company names and private URLs and found none that needed added redactions.`,
		`I performed a manual review and manually scanned 20 records before approving the next step.`,
	)
	if err != nil {
		t.Fatalf("expected validation to pass: %v", err)
	}
}

func TestValidatePublishAttestation(t *testing.T) {
	t.Parallel()

	err := ValidatePublishAttestation(`The user explicitly approved publishing this archive and allowed the upload to proceed.`)
	if err != nil {
		t.Fatalf("expected publish attestation to pass: %v", err)
	}
}

func TestDefaultPublishAttestationPlaceholderPasses(t *testing.T) {
	t.Parallel()

	if err := ValidatePublishAttestation(DefaultPublishAttestationPlaceholder); err != nil {
		t.Fatalf("UI-suggested placeholder must satisfy the validator: %v", err)
	}
}
