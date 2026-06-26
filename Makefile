.PHONY: setup build up down logs status test clean

setup:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo ".env file created from .env.example"; \
	else \
		echo ".env file already exists"; \
	fi

build:
	docker compose build

up:
	docker compose up -d

down:
	docker compose down -v

logs:
	docker compose logs -f

status:
	docker compose ps

clean:
	docker compose down -v
	docker system prune -f
