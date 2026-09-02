.PHONY: run server frontend build

# Runs the API and the UI together for local testing; Ctrl+C stops both.
run:
	@trap 'kill 0' EXIT; \
	$(MAKE) server & \
	$(MAKE) frontend & \
	wait

server:
	go run ./cmd/server

frontend:
	cd frontend && npm run dev

build:
	go build -o bin/server ./cmd/server
	cd frontend && npm run build
