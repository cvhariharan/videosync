build: main.go
	go build -o videosync main.go

install: build
	cp videosync /usr/local/bin