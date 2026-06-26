.PHONY: setup build up down logs status prod-build prod-up prod-down prod-logs prod-status test clean

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

prod-build:
	docker compose -f docker-compose.prod.yml build

prod-up:
	docker compose -f docker-compose.prod.yml up -d

prod-down:
	docker compose -f docker-compose.prod.yml down -v

prod-logs:
	docker compose -f docker-compose.prod.yml logs -f

prod-status:
	docker compose -f docker-compose.prod.yml ps

clean:
	docker compose down -v
	docker compose -f docker-compose.prod.yml down -v
	docker system prune -f

