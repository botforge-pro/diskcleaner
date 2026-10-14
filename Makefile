STATICCHECK_VERSION := v0.8.2
GO_BIN := $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)
STATICCHECK := $(GO_BIN)/staticcheck

.PHONY: tools format lint test-build test build install

tools:
	go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
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
