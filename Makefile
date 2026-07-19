PREFIX     ?= $(HOME)/.local
BIN_DIR    ?= $(PREFIX)/bin
CONFIG_DIR ?= $(HOME)/.config/ov

.PHONY: help install uninstall build test config parity

help:
	@echo "Targets:"
	@echo "  build      Build the Go binary to dist/ov"
	@echo "  install    Build and install ov into BIN_DIR, create config.toml if missing"
	@echo "  uninstall  Remove the installed binary (config left in place)"
	@echo "  config     Create $(CONFIG_DIR)/config.toml if missing (via 'ov init')"
	@echo "  test       Run the Go test suite"
	@echo "  parity     Run scripts/parity-check.sh (SOURCE=/path/to/vault required)"
	@echo ""
	@echo "Variables:"
	@echo "  PREFIX     install prefix (default: \$$HOME/.local)"
	@echo "  BIN_DIR    where to place the ov binary (default: \$$PREFIX/bin)"
	@echo "  CONFIG_DIR where to place the config (default: \$$HOME/.config/ov)"

build:
	@mkdir -p dist
	go build -o dist/ov ./cmd/ov
	@echo "✓ Built dist/ov"

install: build
	@mkdir -p $(BIN_DIR)
	@rm -f $(BIN_DIR)/ov
	@cp dist/ov $(BIN_DIR)/ov
	@echo "✓ Installed $(BIN_DIR)/ov"
	@$(MAKE) config
	@echo ""
	@echo "Next: edit $(CONFIG_DIR)/config.toml to set vault_dir."
	@echo "Then: ov doctor  (smoke test)"

config:
	@mkdir -p $(CONFIG_DIR)
	@$(BIN_DIR)/ov init

uninstall:
	@rm -f $(BIN_DIR)/ov
	@echo "✓ Removed $(BIN_DIR)/ov"
	@echo "Config at $(CONFIG_DIR) left in place. Remove manually if desired."

test:
	go test ./...

parity:
	@test -n "$(SOURCE)" || (echo "usage: make parity SOURCE=/path/to/vault [ARGS='--with-llm']" && exit 2)
	./scripts/parity-check.sh --source "$(SOURCE)" $(ARGS)
