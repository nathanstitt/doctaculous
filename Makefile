.PHONY: build test test-short lint vet fmt tidy clean bench

BIN := bin/doctaculous

# Our packages. We list these explicitly rather than using ./... so unrelated Go
# files that may exist under the working tree (e.g. vendored skill/example assets
# in agent/) never break our build, test, or lint.
PKGS := ./cmd/... ./pkg/... ./testdata/gen/...

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/doctaculous

test:
	go test -race $(PKGS)

test-short:
	go test -short $(PKGS)

bench:
	go test -run '^$$' -bench . -benchmem $(PKGS)

lint: vet
	golangci-lint run $(PKGS)

vet:
	go vet $(PKGS)

fmt:
	gofmt -w .

tidy:
	go mod tidy

clean:
	rm -rf bin
