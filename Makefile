GOLANGCI_LINT_VERSION ?= v2.1.5

init:
	go mod tidy

install-lint-deps:
	(which golangci-lint > /dev/null) || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(GOLANGCI_LINT_VERSION)

lint: install-lint-deps
	golangci-lint run ./...

lint-fix: install-lint-deps
	golangci-lint run ./... --fix

build: init
	bash ./build.sh

web:
	cd web && npm ci --no-audit --no-fund && npm run build

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X sudoku-golang/internal/version.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

bin: web
	mkdir -p build
	go build -trimpath -ldflags "$(LDFLAGS)" -o build/sudoku ./cmd/main.go
	@echo "✅ Бинарник: build/sudoku"

test:
	go test -race ./...

# Бинарники под все платформы в build/dist (без архивов; релизы собирает goreleaser, см. .goreleaser.yaml).
cross: web
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
		echo "→ $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o build/dist/sudoku_$${os}_$${arch}$$ext ./cmd/main.go || exit 1; \
	done

release-snapshot:
	goreleaser release --snapshot --clean

serve: web
	go run ./cmd/main.go serve --log

web-dev:
	cd web && npm run dev

.PHONY: init run generate migrate-create lint lint-fix install-lint-deps build bin web serve web-dev test cross release-snapshot