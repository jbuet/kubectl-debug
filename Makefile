.PHONY: build test clean

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
BINARY_NAME=kubectl-debug

all: test build

build:
	$(GOBUILD) -o $(BINARY_NAME) -v

test:
	$(GOTEST) -v ./test/... ./cmd/...

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
