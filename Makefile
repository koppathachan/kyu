# kyu

SPEC_DIR := spec
GEN_DIR  := gen

.PHONY: generate
generate: ## Regenerate Go code from the contracts in spec/ into gen/
	cd $(SPEC_DIR) && buf generate

.PHONY: generate-check
generate-check: generate ## Fail if regenerating changed the tree (a CI gate)
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "regeneration changed the working tree:"; \
		git status --porcelain; \
		echo; \
		echo "either the generated code is stale, or some of it was committed;"; \
		echo "gen/ is ignored on purpose and must stay that way"; \
		exit 1; \
	fi

.PHONY: lint-proto
lint-proto: ## Lint the contracts
	cd $(SPEC_DIR) && buf lint

.PHONY: breaking
breaking: ## Fail if spec/ breaks the contract published on main
	buf breaking $(SPEC_DIR) --against '.git#branch=main,subdir=spec'

.PHONY: build
build: generate ## Build every binary
	go build ./...

.PHONY: run-engine
run-engine: generate ## Run the engine
	go run ./cmd/kyu-engine

.PHONY: test
test: generate ## Run the tests
	go test ./...

.PHONY: vet
vet: generate ## Vet the Go code
	go vet ./...

.PHONY: clean
clean: ## Remove generated code
	rm -rf $(GEN_DIR)

.PHONY: help
help: ## List targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-16s %s\n", $$1, $$2}'
