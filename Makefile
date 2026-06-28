PROJECT := unobin-library-namecheap
DIR_ROOT := $(realpath $(CURDIR))
DIR_OUT  := _output
UID := $(shell id -u)
GID := $(shell id -g)

CTR_IMAGE_GO   := ghcr.io/cloudboss/docker.io/library/golang:1.26.2-alpine3.23
CTR_IMAGE_LINT := ghcr.io/cloudboss/docker.io/golangci/golangci-lint:v2.11.4-alpine

UNOBIN_VERSION := $(shell awk '/github.com[/]cloudboss[/]unobin v/{print $$2}' go.mod)
DOCGEN ?= go run github.com/cloudboss/cloudboss-docs/unobin/cmd/docgen@main

.DEFAULT_GOAL := help

.PHONY: help docs lint test test-integration-live

help:
	@echo 'Targets:'
	@echo '  docs                         Generate the reference manual.'
	@echo '  lint                         Run golangci-lint in a container.'
	@echo '  test                         Run unit tests on the host.'
	@echo '  test-integration-live        Run integration tests against the live Namecheap API.'

$(DIR_OUT):
	@mkdir -p $(@)

$(DIR_OUT)/%/: $(DIR_OUT)
	@mkdir -p $(DIR_OUT)/$(*)

docs:
	@$(DOCGEN) --root $(DIR_ROOT) --out docs/reference

test:
	@go test -v ./...

lint:
	@docker run --rm \
		-v $(DIR_ROOT):/code:z \
		-w /code $(CTR_IMAGE_LINT) golangci-lint run -v ./...

# Integration tests talk to the real Namecheap API. They need API credentials
# and a disposable test domain on the account, passed through from the
# environment.
test-integration-live: | $(DIR_OUT)/xdg-cache/
	@docker run --rm \
		-v $(DIR_ROOT):/code:z \
		-v $(DIR_ROOT)/$(DIR_OUT)/xdg-cache:/nchome/.cache:z \
		-u $(UID):$(GID) \
		-w /code \
		-e HOME=/nchome \
		-e GOPATH=/code/$(DIR_OUT)/go \
		-e GOCACHE=/code/$(DIR_OUT)/gocache \
		-e UNOBIN_VERSION=$(UNOBIN_VERSION) \
		-e SCENARIO \
		-e NAMECHEAP_USER_NAME \
		-e NAMECHEAP_API_USER \
		-e NAMECHEAP_API_KEY \
		-e NAMECHEAP_CLIENT_IP \
		-e NAMECHEAP_USE_SANDBOX \
		-e NAMECHEAP_TEST_DOMAIN \
		$(CTR_IMAGE_GO) sh -c './tests/integration/run.sh live'
