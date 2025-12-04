AIR := $(shell which air 2>/dev/null || echo $(HOME)/go/bin/air)

.PHONY: dev build install-air

dev: install-air
	$(AIR)

build:
	go build -o saws ./cmd/saws

install-air:
	@test -x $(AIR) || go install github.com/air-verse/air@latest
