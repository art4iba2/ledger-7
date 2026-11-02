# Ledger

## Подготовка из корня проекта

```cmd
docker compose up -d --wait
set "DATABASE_URL=postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable"
set "REDIS_ADDR=localhost:6379"
set "REDIS_DB=0"
set "REDIS_PASSWORD="
go install github.com/pressly/goose/v3/cmd/goose@v3.20.0
set "PATH=%PATH%;%USERPROFILE%\go\bin"
goose -dir ledger/migrations postgres "%DATABASE_URL%" up
cd ledger
go mod tidy
go run ./cmd/ledger
```

Compose создаёт cashapp и именованный том PostgreSQL. При локальной установке
можно создать базу командой `createdb -U postgres cashapp`.
`cmd/ledger` проверяет PostgreSQL, Redis и таблицы и завершает работу.
Затем из папки gateway с теми же переменными запустите `go run .`.

В PowerShell присваивание переменных выглядит так:

```powershell
$env:DATABASE_URL = 'postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable'
$env:REDIS_ADDR = 'localhost:6379'
$env:REDIS_DB = '0'
$env:REDIS_PASSWORD = ''
```

DATABASE_URL имеет приоритет. Иначе используются DB_HOST=localhost,
DB_PORT=5432, DB_USER=postgres, DB_PASS=postgres, DB_NAME=cashapp,
DB_SSLMODE=disable. REDIS_ADDR по умолчанию localhost:6379, REDIS_DB=0,
REDIS_PASSWORD пустой. `.env` автоматически не читается.
При старте выполняется Ping обеих систем и логируется успешное подключение;
ошибка подключения завершает приложение. SQL-пул: open=10, idle=5, lifetime=30m.

## Миграции

В migrations две версии Goose: budgets и expenses, каждая содержит Up и Down.
Применяйте их до запуска Gateway. Из корня репозитория в CMD:

```cmd
goose -dir ledger/migrations postgres "%DATABASE_URL%" up
goose -dir ledger/migrations postgres "%DATABASE_URL%" status
```

Откат одной версии (удаляет таблицу и её данные):

```cmd
goose -dir ledger/migrations postgres "%DATABASE_URL%" down
```

Первый down удаляет expenses, второй — budgets. В PowerShell замените
`"%DATABASE_URL%"` на `"$env:DATABASE_URL"`. Goose установлен в GOPATH/bin
(по умолчанию `%USERPROFILE%\go\bin`).

## Бизнес-правила и API пакета

- SetBudget(Budget) error: Validate + UPSERT уникальной категории.
- ListBudgets() ([]Budget,error): ORDER BY category.
- AddTransaction(Transaction) error: Validate, блокировка категории,
  поиск бюджета, SUM расходов и INSERT в одной транзакции.
- ListTransactions() ([]Transaction,error): ORDER BY date DESC, id DESC.
- GetReportSummary(ctx,from,to): SUM(amount) GROUP BY category для включённого интервала дат.

Категория без бюджета разрешена. Превышение возвращает ErrBudgetExceeded
и откатывает транзакцию. SetBudget использует ту же advisory lock категории.
Валидация требует положительных сумм с максимум двумя десятичными знаками,
непустой категории и корректной непустой даты YYYY-MM-DD.
Если дата пропущена, используется текущая дата UTC.

## Redis

Ключ отчёта: `report:summary:<from>:<to>`, значение — JSON, TTL — 30 секунд.
При попадании логируется cache hit. При отсутствии/ошибке/повреждённом JSON
Ledger выполняет SQL-агрегацию и обновляет кеш. После старта сбой Redis
не блокирует чтение отчёта из PostgreSQL. Отчёт может отставать от записей
на TTL; ListBudgets и ListTransactions не кешируются. Redis DB должен быть
выделен этому набору данных. Данные PostgreSQL переживают перезапуск сервиса.

После запуска Gateway выполните в CMD два раза:

```cmd
curl.exe -i "http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30"
curl.exe -i "http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30"
```

Эффект кеша виден по miss/hit в консоли Gateway. Примеры POST, SQL-проверки
таблиц и сохранности после перезапуска приведены в [корневом README](../README.md).

## Тесты

Из папки ledger:

```cmd
go mod tidy
go test ./... -cover -coverprofile cover.out
go tool cover -html cover.out -o cover.html
```

С реальным отдельным PostgreSQL:

```cmd
set "TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable"
go test -tags integration -v ./...
```

Создайте cashapp_test заранее. Тест использует временную схему и не очищает
рабочие таблицы. Новая версия здесь не запускалась: Go и БД недоступны.
