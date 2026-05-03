.PHONY: docs docs-check

docs:
	go run ./cmd/gendocs ./man/man1

docs-check:
	@tmp=$$(mktemp -d) && \
		go run ./cmd/gendocs $$tmp && \
		diff -ru man/man1 $$tmp && \
		rm -rf $$tmp
