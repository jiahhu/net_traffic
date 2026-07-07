.PHONY: test run build-linux

test:
	go test ./...

run:
	go run ./cmd/nettraffic --mock --db ./nettraffic-demo.db --listen 127.0.0.1:8080

build-linux:
	bash scripts/build-linux.sh

