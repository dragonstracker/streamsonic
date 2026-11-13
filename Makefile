BINARY  := streamsonic
VERSION ?= 1.3.0
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := -trimpath

PLATFORMS := linux/amd64 linux/arm64 linux/arm linux/arm64/v8 darwin/amd64 darwin/arm64

.PHONY: all build vet fmt test clean dist release

all: build

build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

vet:
	go vet ./...

fmt:
	gofmt -l -w .

test:
	go test ./...

dist:
	rm -rf dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/streamsonic-linux-amd64/streamsonic ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/streamsonic-linux-arm64/streamsonic ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm GOARM=6 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/streamsonic-linux-arm/streamsonic ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/streamsonic-darwin-amd64/streamsonic ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/streamsonic-darwin-arm64/streamsonic ./cmd/$(BINARY)
	@for d in dist/streamsonic-*/; do \
		name=$$(basename $$d); \
		cp install.sh README.md LICENSE streamsonic.conf.example $$d; \
		tar -C $$d -czf dist/$$name.tar.gz .; \
	done
	cd dist && sha256sum streamsonic-*.tar.gz > checksums.txt
	@cat dist/checksums.txt

release: dist
	@echo "tag and push: git tag v$(VERSION) && git push origin v$(VERSION)"

clean:
	rm -rf bin dist
