default: help

.PHONY: help
help:	## show this help and exit
	@egrep -h '^[a-zA-Z0-9_.-]+:.*## ' $(MAKEFILE_LIST) | \
	awk 'BEGIN{FS="## " } {t=$$1; sub(/:.*/,"",t); printf "\033[36m  %-25s\033[0m %s\n", t, $$2}'

.PHONY: build
build:	## build
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"'

.PHONY: install
install: ## install
	CGO_ENABLED=0 go install -a -ldflags '-extldflags "-static"'
