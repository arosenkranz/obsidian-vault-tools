PREFIX     ?= $(HOME)/.local
BIN_DIR    ?= $(PREFIX)/bin
CONFIG_DIR ?= $(HOME)/.config/ov

.PHONY: help install uninstall link unlink config check test build gotest

help:
	@echo "Targets:"
	@echo "  install    Symlink ov into BIN_DIR and create config from example if missing"
	@echo "  link       Just create the symlink"
	@echo "  unlink     Remove the symlink"
	@echo "  config     Create $(CONFIG_DIR)/config from example if missing"
	@echo "  check      Syntax-check the scripts"
	@echo "  test       Run the pytest suite (tests/)"
	@echo "  uninstall  unlink + leave config in place"
	@echo ""
	@echo "Variables:"
	@echo "  PREFIX     install prefix (default: \$$HOME/.local)"
	@echo "  BIN_DIR    where to place the ov symlink (default: \$$PREFIX/bin)"
	@echo "  CONFIG_DIR where to place the config (default: \$$HOME/.config/ov)"

install: link config
	@echo ""
	@echo "Next: edit $(CONFIG_DIR)/config to set OV_VAULT_DIR."
	@echo "Then: ov inbox  (smoke test)"

link:
	@mkdir -p $(BIN_DIR)
	@ln -sf $(CURDIR)/bin/vault.sh $(BIN_DIR)/ov
	@chmod +x $(CURDIR)/bin/vault.sh $(CURDIR)/bin/triage_llm.py $(CURDIR)/bin/moc_cleanup.py
	@echo "✓ Linked $(BIN_DIR)/ov → $(CURDIR)/bin/vault.sh"

unlink:
	@rm -f $(BIN_DIR)/ov
	@echo "✓ Removed $(BIN_DIR)/ov"

config:
	@mkdir -p $(CONFIG_DIR)
	@if [ ! -f $(CONFIG_DIR)/config ]; then \
		cp examples/ov.config.example $(CONFIG_DIR)/config; \
		echo "✓ Created $(CONFIG_DIR)/config (edit it to set OV_VAULT_DIR)"; \
	else \
		echo "→ $(CONFIG_DIR)/config already exists, leaving alone"; \
	fi

check:
	@bash -n bin/vault.sh && echo "✓ vault.sh syntax OK"
	@python3 -c "import ast; ast.parse(open('bin/triage_llm.py').read())" && echo "✓ triage_llm.py syntax OK"
	@python3 -c "import ast; ast.parse(open('bin/moc_cleanup.py').read())" && echo "✓ moc_cleanup.py syntax OK"

test:
	python3 -m pytest tests/ -v

uninstall: unlink
	@echo "Config at $(CONFIG_DIR) left in place. Remove manually if desired."

build:
	@mkdir -p dist
	go build -o dist/ov2 ./cmd/ov
	@echo "✓ Built dist/ov2"

gotest:
	go test ./...
