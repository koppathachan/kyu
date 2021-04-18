build:
	./scripts/build.sh

docker-image:
	make build
	./scripts/docker-build.sh

run-pubsub:
	go run ./cmd/pubsub/pubsub.go
run-q:
	go run ./cmd/q/q.go

run: run-q run-pubsub

watch:
	ulimit -n 1000 #increase the file watch limit, might required on MacOS
	reflex -s -r '\.go$$' make run

.PHONY: test
test:
	go test -v -cover ./...

lint:
	golint -set_exit_status ./...

lint-test: lint test
