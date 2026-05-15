PREFIX     ?= $(HOME)/.local
BIN_DIR    ?= $(PREFIX)/bin
CONFIG_DIR ?= $(HOME)/.config/ov

.PHONY: help install uninstall link unlink config check

help:
	@echo "Targets:"
	@echo "  install    Symlink ov into BIN_DIR and create config from example if missing"
	@echo "  link       Just create the symlink"
	@echo "  unlink     Remove the symlink"
	@echo "  config     Create $(CONFIG_DIR)/config from example if missing"
	@echo "  check      Syntax-check both scripts"
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
	@chmod +x $(CURDIR)/bin/vault.sh $(CURDIR)/bin/triage_llm.py
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

uninstall: unlink
	@echo "Config at $(CONFIG_DIR) left in place. Remove manually if desired."
