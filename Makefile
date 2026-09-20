# kyu
#
# Building and running need Go alone: the generated code under kyu/ is
# committed. buf is needed only to change a contract in api/.

API_DIR := api
GEN_DIRS := kyu

# Addresses the run targets use, overridable on the command line.
STORAGE_ADDR ?= 127.0.0.1:50052
ENGINE_ADDR  ?= 127.0.0.1:50051

.PHONY: build
build: ## Build every binary into bin/
	./scripts/build.sh

.PHONY: test
test: ## Run the tests
	go test ./...

.PHONY: vet
vet: ## Vet the Go code
	go vet ./...

.PHONY: run-memstore
run-memstore: ## Run the reference storage server (a fixture, not a backend)
	go run ./cmd/kyu-memstore -listen $(STORAGE_ADDR)

.PHONY: run-kyu
run-kyu: ## Run the engine against a storage server (see STORAGE_ADDR)
	go run ./cmd/kyu -listen $(ENGINE_ADDR) -storage $(STORAGE_ADDR)

.PHONY: run
run: ## Run both servers (make -j2 run)
	$(MAKE) -j2 run-memstore run-kyu

.PHONY: docker-image
docker-image: ## Build a container image per binary
	docker build -t kyu -f build/package/kyu.Dockerfile .
	docker build -t kyu-memstore -f build/package/kyu-memstore.Dockerfile .

.PHONY: generate
generate: ## Regenerate kyu/ from the contracts in api/
	buf generate

.PHONY: generate-check
generate-check: generate ## Fail if kyu/ is not what regeneration produces (a CI gate)
	@if [ -n "$$(git status --porcelain -- $(GEN_DIRS))" ]; then \
		echo "the committed generated code is not what regeneration produces:"; \
		git status --porcelain -- $(GEN_DIRS); \
		echo; \
		echo "run 'make generate' and commit the result"; \
		exit 1; \
	fi

.PHONY: lint-proto
lint-proto: ## Lint the contracts
	buf lint

.PHONY: breaking
breaking: ## Fail if api/ breaks the contract published on main
	buf breaking --against '.git#branch=main'

.PHONY: help
help: ## List targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-16s %s\n", $$1, $$2}'
