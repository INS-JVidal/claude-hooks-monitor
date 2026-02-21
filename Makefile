# Claude Code Hooks Monitor — Makefile
# Usage: make help

ifeq ($(OS),Windows_NT)
  EXE := .exe
else
  EXE :=
endif

BINARY      := bin/monitor$(EXE)
HOOK_CLIENT := hooks/hook-client$(EXE)
PORT        ?= 8080
GO          := $(shell which go 2>/dev/null || echo /usr/local/go/bin/go)
HOOK_DIR    := hooks
CONF        := $(HOOK_DIR)/hook_monitor.conf

.PHONY: help deps build build-hook-client run run-ui run-background test test-api send-test-hook \
        clean install check stats show-config reset-config show-hooks-config

help: ## Show all targets with descriptions
	@echo ""
	@echo "  Claude Code Hooks Monitor"
	@echo "  ─────────────────────────"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@echo ""

deps: ## Install Go deps
	@echo "Checking Go dependencies..."
	$(GO) mod tidy
	@command -v jq >/dev/null 2>&1 || { echo "Warning: jq not found. Install: sudo apt install jq"; }
	@echo "Dependencies OK."

build: ## Build monitor server and hook client
	@mkdir -p bin
	$(GO) build -ldflags="-s -w" -o $(BINARY) .
	$(GO) build -ldflags="-s -w" -o $(HOOK_CLIENT) ./cmd/hook-client
	@echo "Built $(BINARY) and $(HOOK_CLIENT)"

build-hook-client: ## Build only the hook client binary
	$(GO) build -ldflags="-s -w" -o $(HOOK_CLIENT) ./cmd/hook-client
	@echo "Built $(HOOK_CLIENT)"

run: ## Run server (foreground, console output)
	@echo "Starting monitor on port $(PORT)..."
	PORT=$(PORT) $(GO) run .

run-ui: build ## Run server with interactive tree UI
	PORT=$(PORT) ./$(BINARY) --ui

run-background: ## Run server in background (output to monitor.log)
ifeq ($(OS),Windows_NT)
	@echo "run-background is not supported on Windows. Use 'make run' in a separate terminal."
else
	@mkdir -p bin
	$(GO) build -ldflags="-s -w" -o $(BINARY) .
	PORT=$(PORT) nohup ./$(BINARY) > monitor.log 2>&1 &
	@sleep 1
	@echo "Server started in background (PID $$(lsof -ti:$(PORT) 2>/dev/null || echo '?'))"
	@echo "Log: tail -f monitor.log"
endif

test: ## Run full test suite (requires running server)
	@./test-hooks.sh

test-api: ## Test API endpoints with curl
	@echo "Health:"
	@curl -sf http://localhost:$(PORT)/health | python3 -m json.tool
	@echo ""
	@echo "Stats:"
	@curl -sf http://localhost:$(PORT)/stats | python3 -m json.tool
	@echo ""
	@echo "Last 3 events:"
	@curl -sf "http://localhost:$(PORT)/events?limit=3" | python3 -m json.tool

send-test-hook: ## Send a single test event
	@curl -s -X POST http://localhost:$(PORT)/hook/PreToolUse \
		-H "Content-Type: application/json" \
		-d '{"hook_event_name":"PreToolUse","session_id":"manual","cwd":"/tmp","permission_mode":"default","tool_name":"Bash","tool_input":{"command":"echo hello"}}' \
		| python3 -m json.tool

clean: ## Remove build artifacts and logs
	rm -rf bin/
	rm -f $(HOOK_CLIENT)
	rm -f monitor.log
	@echo "Cleaned."

install: build ## Install binaries to ~/bin
	@mkdir -p ~/bin
	cp $(BINARY) ~/bin/claude-hooks-monitor
	@echo "Installed to ~/bin/claude-hooks-monitor"

check: ## Check if server is running
	@curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1 \
		&& echo "Server is running on port $(PORT)" \
		|| echo "Server is NOT running on port $(PORT)"

stats: ## Show hook statistics
	@curl -sf http://localhost:$(PORT)/stats | python3 -m json.tool

show-config: ## Display current hook toggle state
	@echo ""
	@echo "  Hook Monitor Configuration ($(CONF))"
	@echo "  ────────────────────────────────────"
	@grep -E '^[A-Za-z]' $(CONF) | while read line; do \
		if echo "$$line" | grep -q "= yes"; then \
			echo "  \033[32m$$line\033[0m"; \
		else \
			echo "  \033[31m$$line\033[0m"; \
		fi; \
	done
	@echo ""

reset-config: ## Reset hook config to all-enabled defaults
	@sed -i 's/= no/= yes/g' $(CONF)
	@echo "All hooks reset to enabled."

show-hooks-config: ## Print hooks JSON snippet for .claude/settings.json
	@HOOK_CLIENT="$(CURDIR)/hooks/hook-client$(EXE)"; \
	echo ""; \
	echo "  Add this to your project's .claude/settings.json to monitor hooks:"; \
	echo "  (hook-client path: $$HOOK_CLIENT)"; \
	echo ""; \
	echo '  "hooks": {'; \
	first=true; \
	for event in SessionStart SessionEnd UserPromptSubmit PreToolUse PostToolUse PostToolUseFailure \
	             Notification PermissionRequest Stop SubagentStart SubagentStop TeammateIdle \
	             TaskCompleted ConfigChange PreCompact; do \
		if [ "$$first" = true ]; then first=false; else echo ','; fi; \
		case "$$event" in \
			PreToolUse|PostToolUse|PostToolUseFailure) \
				printf '    "%s": [{"matcher": "*", "hooks": [{"type": "command", "command": "\"%s\""}]}]' "$$event" "$$HOOK_CLIENT" ;; \
			*) \
				printf '    "%s": [{"hooks": [{"type": "command", "command": "\"%s\""}]}]' "$$event" "$$HOOK_CLIENT" ;; \
		esac; \
	done; \
	echo ""; \
	echo '  }'; \
	echo ""
