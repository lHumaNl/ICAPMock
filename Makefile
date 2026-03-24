.PHONY: build run test test-coverage lint clean docker-build docker-run generate-cert install-completion help

# Version from git tags
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION)"

# Binary and directories
BINARY_NAME := icap-mock
BIN_DIR := bin
CMD_DIR := cmd/icap-mock

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./$(CMD_DIR)

## run: Run the application locally
run:
	go run ./$(CMD_DIR) --config configs/config.yaml

## test: Run all tests with race detection
test:
	go test -v -race -coverprofile=coverage.out ./...

## test-coverage: Generate HTML coverage report
test-coverage:
	go tool cover -html=coverage.out

## lint: Run golangci-lint
lint:
	golangci-lint run

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR)/
	rm -f coverage.out coverage.html

## docker-build: Build Docker image
docker-build:
	docker build -t $(BINARY_NAME):$(VERSION) .

## docker-run: Run Docker container
docker-run:
	docker run -p 1344:1344 -p 9090:9090 $(BINARY_NAME):$(VERSION)

## generate-cert: Generate TLS certificates for the server
generate-cert:
	@echo "Generating TLS certificates..."
	@./scripts/generate_cert.sh

## fmt: Format code
fmt:
	go fmt ./...

## vet: Run go vet
vet:
	go vet ./...

## mod-tidy: Tidy dependencies
mod-tidy:
	go mod tidy

## all: Run all quality checks
all: fmt vet lint test

## install-completion: Install shell completion for the current shell
install-completion:
	@echo "Installing shell completion..."
	@if [ -n "$$BASH_VERSION" ]; then \
		echo "Detected bash shell"; \
		mkdir -p ~/.bash_completion.d 2>/dev/null; \
		cp scripts/completion.bash ~/.bash_completion.d/icap-mock || \
		mkdir -p ~/.local/share/bash-completion/completions 2>/dev/null && \
		cp scripts/completion.bash ~/.local/share/bash-completion/completions/icap-mock; \
		echo "Bash completion installed. Run 'source ~/.bashrc' or restart your shell."; \
	elif [ -n "$$ZSH_VERSION" ]; then \
		echo "Detected zsh shell"; \
		mkdir -p ~/.zsh/completion 2>/dev/null; \
		cp scripts/completion.zsh ~/.zsh/completion/_icap-mock; \
		echo "fpath=(~/.zsh/completion \$$fpath)" >> ~/.zshrc; \
		echo "autoload -U compinit && compinit" >> ~/.zshrc; \
		echo "Zsh completion installed. Run 'exec zsh' or restart your shell."; \
	elif [ -n "$$FISH_VERSION" ]; then \
		echo "Detected fish shell"; \
		mkdir -p ~/.config/fish/completions 2>/dev/null; \
		cp scripts/completion.fish ~/.config/fish/completions/icap-mock.fish; \
		echo "Fish completion installed. Restart your shell or run 'fish'."; \
	else \
		echo "Unsupported shell. Please install manually:"; \
		echo "  Bash: cp scripts/completion.bash ~/.bash_completion.d/icap-mock"; \
		echo "  Zsh:  cp scripts/completion.zsh ~/.zsh/completion/_icap-mock"; \
		echo "  Fish: cp scripts/completion.fish ~/.config/fish/completions/icap-mock.fish"; \
	fi

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /' | column -t -s ':'
