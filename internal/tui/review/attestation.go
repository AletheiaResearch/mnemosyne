package review

func AttestationScreen() Screen {
	return New("Attestation", "Review requires identity-scan, entity-scan, and manual-review attestations before publication is unlocked.")
}
