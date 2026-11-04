STATICCHECK_VERSION := v0.8.2
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)

VERSION = $(shell sed -n 's/^\#\# \[\([0-9.]*\)\].*/\1/p' CHANGELOG.md | head -1)

.PHONY: tools format lint test-build test build install release

tools:
	pip install --upgrade git+https://github.com/botforge-pro/commentcensor.git

format:
	gofmt -w .

lint:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	$(STATICCHECK) ./...
	commentcensor . .github

test-build:
	go build ./...
	go test -run '^$$' ./...

test:
	go test -count=1 -run '$(TEST)' ./...

build:
	go build -o diskcleaner .

install:
	go install .

# A release tags the checked, pushed main with v and the newest CHANGELOG version, and its
# GitHub release carries that version's CHANGELOG section as the notes.
release: lint test
	@test -z "$$(git status --porcelain)" || { echo "Commit first: the release is the tag on what is in git."; exit 1; }
	@git fetch -q origin main
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { echo "Release from main as pushed."; exit 1; }
	git tag -a 'v$(VERSION)' -m 'v$(VERSION)'
	git push origin 'v$(VERSION)'
	awk '/^## \[$(VERSION)\]/{f=1;next} /^## \[/{f=0} f' CHANGELOG.md | gh release create 'v$(VERSION)' --title 'v$(VERSION)' --notes-file -
