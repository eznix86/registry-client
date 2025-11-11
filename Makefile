test:
	golangci-lint run -c .golangci.yml ./...
	go test -cover ./... -v

ci:
	act --quiet --container-architecture linux/amd64 -P ubuntu-latest=catthehacker/ubuntu:act-latest --bind --reuse --pull=false | grep -v '::'
ci/pr:
	act pull_request --quiet --container-architecture linux/amd64 -P ubuntu-latest=catthehacker/ubuntu:act-latest --bind --reuse --pull=false | grep -v '::'

coverage:
	go test -coverprofile=/tmp/coverage.out ./... ; go tool cover -html=/tmp/coverage.out
