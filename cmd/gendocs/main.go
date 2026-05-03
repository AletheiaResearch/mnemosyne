// Command gendocs writes mandoc(1) man pages for the mnemosyne CLI to a
// target directory (default ./man/man1) using cobra/doc.GenManTree.
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra/doc"

	"github.com/AletheiaResearch/mnemosyne/internal/cli"
)

func main() {
	out := "man/man1"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", out, err)
	}
	date := docDate()
	header := &doc.GenManHeader{
		Title:   "MNEMOSYNE",
		Section: "1",
		Source:  "Mnemosyne",
		Manual:  "Mnemosyne Manual",
		Date:    &date,
	}
	if err := doc.GenManTree(cli.NewDocsRoot(), header, out); err != nil {
		log.Fatalf("gen man tree: %v", err)
	}
}

// docDate returns the date stamped into each man page header. The default
// is the Unix epoch so committed artifacts stay diff-stable across runs;
// release builds get the real commit time via SOURCE_DATE_EPOCH (set
// automatically by goreleaser, or by reproducible-build tooling).
func docDate() time.Time {
	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(epoch, 0).UTC()
		}
	}
	return time.Unix(0, 0).UTC()
}
