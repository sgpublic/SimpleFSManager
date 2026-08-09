VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -X github.com/sgpublic/simplefsmanager/internal/buildinfo.Version=$(VERSION)
TAGS ?=

.PHONY: build frontend

build: frontend
	go build $(if $(TAGS),-tags "$(TAGS)") -ldflags "$(LDFLAGS)" -o simplefsmanager ./cmd/simplefsmanager

frontend:
	cd web && npm run build
