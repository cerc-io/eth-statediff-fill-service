BIN = $(GOPATH)/bin

# Tools

## Migration tool
GOOSE = $(BIN)/goose
$(BIN)/goose:
	go get -u github.com/pressly/goose/cmd/goose

.PHONY: installtools
installtools: | $(LINT) $(GOOSE)
	echo "Installing tools"

.PHONY: test
test: | $(GOOSE)
	go vet ./...
	go fmt ./...
	go run github.com/onsi/ginkgo/ginkgo  -r --skipPackage=test

.PHONY: integrationtest
integrationtest: | $(GOOSE)
	go vet ./...
	go fmt ./...
	go run github.com/onsi/ginkgo/ginkgo  -r test/ -v

build:
	go fmt ./...
	GO111MODULE=on go build

## Build docker image
.PHONY: docker-build
docker-build:
	docker build -t cerc-io/eth-statediff-fill-service .
