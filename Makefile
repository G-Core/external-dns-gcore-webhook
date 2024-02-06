GO_TEST = go run gotest.tools/gotestsum --format pkgname

LICENCES_IGNORE_LIST = $(shell cat licences/licences-ignore-list.txt)

ifndef $(GOPATH)
    GOPATH=$(shell go env GOPATH)
    export GOPATH
endif

ARTIFACT_NAME = external-dns-gcore-webhook

# logging
LOG_LEVEL = debug
LOG_ENVIRONMENT = production
LOG_FORMAT = auto

REGISTRY ?= ghcr.io
IMAGE_NAME ?= kokizzu/external-dns-gcore-webhook
IMAGE_TAG ?= latest
IMAGE = $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

show: ## Show variables
	@echo "GOPATH: $(GOPATH)"
	@echo "ARTIFACT_NAME: $(ARTIFACT_NAME)"
	@echo "REGISTRY: $(REGISTRY)"
	@echo "IMAGE_NAME: $(IMAGE_NAME)"
	@echo "IMAGE_TAG: $(IMAGE_TAG)"
	@echo "IMAGE: $(IMAGE)"


##@ Code analysis

.PHONY: fmt
fmt: ## Run gofumpt against code.
	go run mvdan.cc/gofumpt -w .

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint against code.
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run --timeout 2m

.PHONY: static-analysis
static-analysis: lint vet ## Run static analysis against code.

##@ GO

.PHONY: clean
clean: ## Clean the build directory
	rm -rf ./dist
	rm -rf ./build
	rm -rf ./vendor

.PHONY: build
build: ## Build the binary
	LOG_LEVEL=$(LOG_LEVEL) LOG_ENVIRONMENT=$(LOG_ENVIRONMENT) LOG_FORMAT=$(LOG_FORMAT) CGO_ENABLED=0 go build -mod \
	vendor -o $(ARTIFACT_NAME) .

.PHONY: run
run:build ## Run the binary on local machine
	external-dns-gcore-webhook

##@ Docker

.PHONY: docker-build
docker-build: build ## Build the docker image
	docker build ./ -f Dockerfile -t $(IMAGE)

.PHONY: docker-push
docker-push: ## Push the docker image
	docker push $(IMAGE)

##@ Test

.PHONY: unit-test
unit-test: ## Run unit tests
	$(GO_TEST) -- -race ./... -count=1 -short -cover -coverprofile unit-test-coverage.out


##@ Release

.PHONY: release-check
release-check: ## Check if the release will work
	GITHUB_SERVER_URL=github.com GITHUB_REPOSITORY=kokizzu/external-dns-gcore-webhook REGISTRY=$(REGISTRY) IMAGE_NAME=$(IMAGE_NAME) goreleaser release --snapshot --clean --skip=publish

