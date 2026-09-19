.PHONY: build clean

build:
	mkdir -p build
	go mod download
	go build -o build/server ./server
	go build -o build/client ./client
	cp server/config-example.json build/server-config-example.json
	cp client/config-example.json build/client-config-example.json

clean:
	go clean
	rm -rf build