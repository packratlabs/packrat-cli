# Linters are tools, not dependencies of the binary. `go run pkg@version` keeps them out of go.mod.
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.8.1
GOLANGCI    := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GORELEASER  := go run github.com/goreleaser/goreleaser/v2@v2.18.3

.PHONY: build check corpus release-dry

build:
	CGO_ENABLED=0 go build -o bin/packrat .

check:
	@out="$$(gofmt -l main.go main_test.go internal)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	$(STATICCHECK) ./...
	$(GOLANGCI) run
	go test ./... -count=1

# Clones each repository of corpus/repos.txt at its pinned commit. This is the only target that
# uses the network. The go.mod makes Go tools treat corpus/src as another module and skip it.
corpus:
	@mkdir -p corpus/src && printf 'module corpus\n' > corpus/src/go.mod
	@grep -v '^#' corpus/repos.txt | while read -r name url commit; do \
		case "$$url" in local:*) continue ;; esac; \
		dir="corpus/src/$$name"; \
		if [ -d "$$dir/.git" ] && [ "$$(git -C "$$dir" rev-parse HEAD)" = "$$commit" ]; then \
			echo "$$name is at $$commit"; continue; \
		fi; \
		echo "cloning $$name at $$commit"; \
		rm -rf "$$dir" && git init -q "$$dir" && git -C "$$dir" remote add origin "$$url" \
			&& git -C "$$dir" fetch -q --depth 1 --filter=blob:none origin "$$commit" \
			&& git -C "$$dir" -c advice.detachedHead=false checkout -q FETCH_HEAD || exit 1; \
	done

# Builds the five release binaries, their archives and checksums.txt into dist/. A snapshot
# publishes nothing: no release, no tag, no upload.
release-dry:
	$(GORELEASER) check
	PACKRAT_GO="go$$(sed -n 's/^go //p' go.mod)" $(GORELEASER) release --snapshot --clean
	@ls dist/*.tar.gz dist/*.zip dist/checksums.txt
