.PHONY: test vet fmt deps-up deps-down

test:
	go test ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

deps-up:
	docker compose --env-file .env -f deploy/docker-compose.yml up -d

deps-down:
	docker compose --env-file .env -f deploy/docker-compose.yml down
