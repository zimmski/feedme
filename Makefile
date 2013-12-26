.PHONY: clean install lint

clean:
	go clean github.com/zimmski/feedme/feedme-crawler
	go clean github.com/zimmski/feedme/feedme-server
fmt:
	gofmt -l -w -tabs=true .
install:
	go install github.com/zimmski/feedme/feedme-crawler
	go install github.com/zimmski/feedme/feedme-server
lint:
	golint .
	go tool vet -all=true -v=true .

