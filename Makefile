.PHONY: up down test demo build

up:
	cd deploy/docker && docker compose up --build

down:
	cd deploy/docker && docker compose down

test:
	go test -count=1 ./...
	go test -race -count=1 ./tests/...

build:
	go build ./...
	docker build -f deploy/docker/Dockerfile.api -t agentguard-api .

demo:
	python3 examples/simple-agent/demo.py
