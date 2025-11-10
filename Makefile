test:
	golangci-lint run -c .golangci.yml ./...
	go test -cover ./... -v

ci:
	act --container-architecture linux/amd64
