.PHONY: build test install clean

build:
	go build -o bin/gt .

test:
	go test ./...

install: build
	cp bin/gt /usr/local/bin/gt

clean:
	rm -rf bin/
