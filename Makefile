# kyu
#
# Building and running need Go alone: the generated code in gen/ is committed.
# buf is needed only to change a contract in proto/.

PROTO_DIR := proto
GEN_DIR   := gen

.PHONY: build
build: ## Build every binary
	go build ./...

.PHONY: test
test: ## Run the tests
	go test ./...

.PHONY: vet
vet: ## Vet the Go code
	go vet ./...

.PHONY: run-engine
run-engine: ## Run the engine
	go run ./cmd/kyu-engine

.PHONY: generate
generate: ## Regenerate gen/ from the contracts in proto/
	cd $(PROTO_DIR) && buf generate

.PHONY: generate-check
generate-check: generate ## Fail if gen/ is not what regeneration produces (a CI gate)
	@if [ -n "$$(git status --porcelain -- $(GEN_DIR))" ]; then \
		echo "the committed generated code is not what regeneration produces:"; \
		git status --porcelain -- $(GEN_DIR); \
		echo; \
		echo "run 'make generate' and commit the result"; \
		exit 1; \
	fi

.PHONY: lint-proto
lint-proto: ## Lint the contracts
	cd $(PROTO_DIR) && buf lint

.PHONY: breaking
breaking: ## Fail if proto/ breaks the contract published on main
	buf breaking $(PROTO_DIR) --against '.git#branch=main,subdir=proto'

.PHONY: help
help: ## List targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-16s %s\n", $$1, $$2}'
