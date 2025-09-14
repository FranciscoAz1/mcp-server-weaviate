ifeq ($(OS),Windows_NT)
    EXT = .exe
else
    EXT =
endif

build:
	go build -o client/mcp-server$(EXT) .

run-client:
	cd client && go run client.go
