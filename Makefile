VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -X github.com/sgpublic/simplefsmanager/internal/buildinfo.Version=$(VERSION)

.PHONY: build frontend

build: frontend
	go build -ldflags "$(LDFLAGS)" -o simplefsmanager ./cmd/simplefsmanager

frontend:
	cd web && npm run build
