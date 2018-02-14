aws: build
	zip main.zip main

build:
	GOOS=linux GOARCH=amd64 go build -o main main.go

clean:
	rm main
	rm main.zip

