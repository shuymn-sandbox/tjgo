.PHONY: compile
compile:
	protoc api/log/v1/*.proto --go_out=. --go_opt=paths=source_relative --proto_path=.

.PHONY: test
test:
	go test -race ./...

.PHONY: lint
lint:
	buf lint && golangci-lint run ./...
