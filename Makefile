NAME=ducklett
TAG?=local-0.1.8
ENVVAR=CGO_ENABLED=0
GOOS?=linux
GOARCH?=$(shell go env GOARCH)
IMAGE?=gitlab.com/tmobile/conducktor/infrastructure/$(NAME)
TEST_UTILITY_NAME=update-azure-image-tags

build: 
	$(ENVVAR) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(NAME)

build-test-utility: 
	$(ENVVAR) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(TEST_UTILITY_NAME) test-utility/cmd/main.go 

push-image-kind: 
	kind load docker-image ${IMAGE}:${TAG}

clean:
	rm -f $(NAME)

format:
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -d {} + | tee /dev/stderr)" || \
	test -z "$$(find . -path ./vendor -prune -type f -o -name '*.go' -exec gofmt -s -w {} + | tee /dev/stderr)"

docker-build:
	docker build -t ${IMAGE}:${TAG} -f Dockerfile.local --build-arg RELEASE_VERSION=$(TAG) --build-arg RELEASE_NAME=$(NAME) .

containerize:
	docker build -t ${IMAGE}:${TAG} .

validate:
	go fmt && \
    go vet && \
    go test -race

test:
	go test -race

.PHONY: all build test-unit clean format execute-release dev-release docker-builder build-in-docker release generate push-image push-manifest
