default: help

.PHONY: build
build: ## build
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"'

.PHONY: install
install: ## install
	CGO_ENABLED=0 go install -a -ldflags '-extldflags "-static"'

.PHONY: upx
upx: build ## upx
	upx -9 mkctx


.PHONY: help
help: ## show this help
	@awk '\
	/^[a-zA-Z0-9_.-]+:/ { \
		t=$$0; sub(/:.*/,"",t); \
		if (t ~ /^\./) next; \
		h=""; \
		if ($$0 ~ /[[:space:]]##[[:space:]]+[^[:space:]]/) { \
			h=$$0; sub(/.*[[:space:]]##[[:space:]]+/,"",h); \
			sub(/[ \t]+$$/,"",h); \
		} \
		printf "\x1b[36m  %-25s\x1b[0m %s\n", t, h; \
	}' $(MAKEFILE_LIST)
