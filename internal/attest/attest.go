package attest

import (
	"os"
	"time"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

func BuildRecords(path, fullName string, skipName bool, identity, entity, manual string) (Report, *config.ReviewerStatements, *config.VerificationRecord, *config.LastAttest, error) {
	if err := ValidateStatements(fullName, skipName, identity, entity, manual); err != nil {
		return Report{}, nil, nil, nil, err
	}

	report, err := ScanFile(path, fullName, skipName)
	if err != nil {
		return Report{}, nil, nil, nil, err
	}
	report.ManualSampleCount = ParseManualSampleCount(manual)

	info, err := os.Stat(path)
	if err != nil {
		return Report{}, nil, nil, nil, err
	}

	statements := &config.ReviewerStatements{
		IdentityScan:    identity,
		EntityInterview: entity,
		ManualReview:    manual,
	}
	verification := &config.VerificationRecord{
		FullName:           fullName,
		NameScanSkipped:    skipName,
		FullNameMatchCount: report.NameScan.Count,
		ManualSampleCount:  report.ManualSampleCount,
	}
	lastAttest := &config.LastAttest{
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		FilePath:          path,
		BuiltInFindings:   !report.Findings.Empty(),
		FullName:          fullName,
		NameScanSkipped:   skipName,
		FullNameMatches:   report.NameScan.Count,
		ManualSampleCount: report.ManualSampleCount,
		FileSize:          info.Size(),
	}
	return report, statements, verification, lastAttest, nil
}
