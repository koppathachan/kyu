build:
	./scripts/build.sh

docker-image:
	make build
	./scripts/docker-build.sh

run-sub:
	go run ./cmd/sub/sub.go
run-pub:
	go run ./cmd/pub/pub.go
run-q:
	go run ./cmd/q/q.go

run: run-q run-sub run-pub

watch:
	ulimit -n 1000 #increase the file watch limit, might required on MacOS
	reflex -s -r '\.go$$' make run

.PHONY: test
test:
	go test -v -cover ./...

lint:
	golint -set_exit_status ./...

lint-test: lint test
