.PHONY: help install-nix shell build lint fmt test proto mocks

help:
	@echo "Targets: install-nix shell build lint fmt test proto mocks"

install-nix:
	@./scripts/run_task.sh install-nix

shell:
	@nix develop

build:
	@./scripts/run_task.sh build

lint:
	@./scripts/run_task.sh lint-all

fmt:
	@go fmt ./...

test:
	@./scripts/run_task.sh test-e2e

proto:
	@./scripts/run_task.sh generate-protobuf

mocks:
	@./scripts/run_task.sh generate-mocks
