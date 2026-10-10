# Linters are tools, not dependencies of the binary. `go run pkg@version` keeps them out of go.mod.
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.8.1
GOLANGCI    := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

.PHONY: build check corpus release-dry

build:
	CGO_ENABLED=0 go build -o bin/packrat .

check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	$(STATICCHECK) ./...
	$(GOLANGCI) run
	go test ./... -count=1

corpus:
	@echo "make corpus is milestone M4. It is not built yet."; exit 1

release-dry:
	@echo "make release-dry is milestone M5. It is not built yet."; exit 1
