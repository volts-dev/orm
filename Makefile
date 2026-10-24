# 方言矩阵（MySQL 兼容 P0，2026-10-24）：同一套用例在 SQLite / PostgreSQL / MySQL 上各跑一遍。
#   make test-db-up        起 docker 里的 MySQL 8.0 与 PG 16（test/docker-compose.test.yml，端口 33306 / 54320）
#   make test-matrix       三个方言依次跑；也可以单跑 test-sqlite / test-postgres / test-mysql
# 数据源见 testdb_test.go（ORM_TEST_DB、ORM_TEST_PG、ORM_TEST_MYSQL）。直连本机 PG 的老用例不在矩阵里
# （test/matrix_tests.py 排除），照旧由 go test ./... 跑。
GO      ?= go
COMPOSE  = docker compose -f test/docker-compose.test.yml
MATRIX   = $(shell python3 test/matrix_tests.py .)

.PHONY: test-db-up test-db-down test-sqlite test-postgres test-mysql test-matrix

test-db-up:
	$(COMPOSE) up -d --wait

test-db-down:
	$(COMPOSE) down

test-sqlite:
	@ORM_TEST_DB=sqlite $(GO) test . -count=1 -run '$(MATRIX)'

test-postgres:
	@ORM_TEST_DB=postgres $(GO) test . -count=1 -run '$(MATRIX)'

test-mysql:
	@ORM_TEST_DB=mysql $(GO) test . -count=1 -run '$(MATRIX)'

test-matrix: test-sqlite test-postgres test-mysql
