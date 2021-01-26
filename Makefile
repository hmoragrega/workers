
COVERAGE_FILE = coverage.out

.PHONY: test
test: unit vet fmt

.PHONY: unit
unit:
	@go test -race -v -coverpkg=./... -coverprofile=$(COVERAGE_FILE) ./...

.PHONY: vet
vet:
	@go vet ./...

.PHONY: fmt
fmt:
	$(eval files_to_format := $(shell go fmt $(shell go list ./... | grep -v /vendor/)))
	@if [[ ! -z "$(files_to_format)" ]]; then \
		echo 'Format the following files:'; \
		echo $(files_to_format); \
		exit 1 \
	;fi

.PHONY: clean
clean:
	@rm -rf $(COVERAGE_FILE)

.PHONY: coverage
coverage: test
	@go tool cover -func=$(COVERAGE_FILE)

.PHONY: coverage-html
coverage-html: test
	@go tool cover -html=$(COVERAGE_FILE)
