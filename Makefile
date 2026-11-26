STATICCHECK_VERSION := v0.8.2
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
README_CHECK := TestTheREADMEListsTheStepsAsTheirFilesSay

VERSION = $(shell sed -n 's/^\#\# \[\([0-9.]*\)\].*/\1/p' CHANGELOG.md | head -1)

.PHONY: tools format lint test-build test test-run build install install-release release readme

tools:
	pip install --upgrade git+https://github.com/botforge-pro/commentcensor.git
	go install github.com/botforge-pro/time-limit@v0.1.0

format:
	gofmt -w .

lint:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	$(STATICCHECK) ./...
	commentcensor . .github
	go test -count=1 -v -run '^$(README_CHECK)$$' . | grep -q -- '--- PASS: $(README_CHECK) ' || { echo "the README check did not pass: run make readme, or name the test in README_CHECK"; exit 1; }

test-build:
	go build ./...
	go test -run '^$$' ./...

test:
	time-limit 560 $(MAKE) --no-print-directory test-run

test-run:
	go test -count=1 -run '$(TEST)' -skip '^$(README_CHECK)$$' ./...

build:
	go build -o diskcleaner .

readme:
	UPDATE_README=1 go test -count=1 -run '^$(README_CHECK)$$' .

install:
	go install .

install-release:
	go install github.com/botforge-pro/diskcleaner@v$(VERSION)

# A release tags the checked, pushed main with v and the newest CHANGELOG version, and its
# GitHub release carries that version's CHANGELOG section as the notes.
release: lint test
	@test -z "$$(git status --porcelain)" || { echo "Commit first: the release is the tag on what is in git."; exit 1; }
	@git fetch -q origin main
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { echo "Release from main as pushed."; exit 1; }
	@test "$$(gh run list --workflow CI --commit "$$(git rev-parse HEAD)" --json conclusion -q '.[0].conclusion')" = success || { echo "Release what CI passed: wait for its run on this commit, or fix it."; exit 1; }
	git tag -a 'v$(VERSION)' -m 'v$(VERSION)'
	git push origin 'v$(VERSION)'
	awk '/^## \[$(VERSION)\]/{f=1;next} /^## \[/{f=0} f' CHANGELOG.md | gh release create 'v$(VERSION)' --title 'v$(VERSION)' --notes-file -
