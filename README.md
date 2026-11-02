# Ledger и Gateway: PostgreSQL + Redis

Ledger хранит бюджеты и расходы в PostgreSQL. Отчёты кешируются в Redis
на 30 секунд. Gateway предоставляет прежние HTTP-эндпоинты на `:8080`.
Ledger импортируется в Gateway как локальный Go-модуль через `replace`;
отдельный HTTP-процесс Ledger в этой архитектуре не нужен.

Требуются Go 1.20+, Docker Desktop с Compose и доступ к загрузке Go-модулей.
Версии библиотек закреплены в `go.mod`. После распаковки выполните
`go mod tidy` в обоих модулях: команда загрузит зависимости и создаст `go.sum`.

## 1. PostgreSQL и Redis

Команды выполняются из корня проекта. Это примеры для Windows CMD:

```cmd
docker compose up -d --wait
set "DATABASE_URL=postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable"
set "REDIS_ADDR=localhost:6379"
set "REDIS_DB=0"
set "REDIS_PASSWORD="
```

Compose создаёт БД `cashapp` автоматически и сохраняет PostgreSQL в именованном
томе `postgres-data`. Обычный `docker compose down` сохраняет том.

Если работаете в PowerShell, задайте переменные так:

```powershell
$env:DATABASE_URL = 'postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable'
$env:REDIS_ADDR = 'localhost:6379'
$env:REDIS_DB = '0'
$env:REDIS_PASSWORD = ''
```

Переменные задаются в том окне, из которого запускается Go/Goose.
Приложение не загружает `.env` автоматически. Настройки Compose — для локальной
разработки. Отдельные БД приложения должны использовать отдельные Redis DB,
чтобы одинаковые ключи отчётов не пересекались.

| Переменная | Значение по умолчанию |
| --- | --- |
| DATABASE_URL | Приоритет над всеми DB_* |
| DB_HOST / DB_PORT | localhost / 5432 |
| DB_USER / DB_PASS | postgres / postgres |
| DB_NAME / DB_SSLMODE | cashapp / disable |
| REDIS_ADDR | localhost:6379 |
| REDIS_DB | 0 |
| REDIS_PASSWORD | пустая строка |

Если DATABASE_URL отсутствует, DSN собирается из DB_* с экранированием пароля.
Пул SQL: максимум 10 открытых и 5 свободных соединений, lifetime 30 минут.
При старте выполняются Ping PostgreSQL и Redis; ошибка завершает приложение.

## 2. Установить Goose и применить миграции

```cmd
go install github.com/pressly/goose/v3/cmd/goose@v3.20.0
set "PATH=%PATH%;%USERPROFILE%\go\bin"
goose -dir ledger/migrations postgres "%DATABASE_URL%" up
```

В PowerShell команда миграции:

```powershell
goose -dir ledger/migrations postgres "$env:DATABASE_URL" up
```

Goose использует один SQL-файл с секциями `Up` и `Down` для каждой версии.
Здесь две версии: `00001_create_budgets.sql` и `00002_create_expenses.sql`.
В таблицах созданы ограничения из задания; дополнительно есть индекс
`expenses(category,date)`. `up` применяет обе версии.

Откат последней миграции (expenses), затем при необходимости budgets:

```cmd
goose -dir ledger/migrations postgres "%DATABASE_URL%" down
goose -dir ledger/migrations postgres "%DATABASE_URL%" down
```

Откат удаляет соответствующие таблицы и их данные. После отката перед
запуском снова выполните `up`. В PowerShell используйте `$env:DATABASE_URL`.
Миграции выполняются **до** запуска приложения и не применяются автоматически.

При локальном PostgreSQL без Docker базу можно создать так:

```cmd
createdb -h localhost -p 5432 -U postgres cashapp
```

## 3. Проверить Ledger и запустить Gateway

В том же окне с переменными окружения:

```cmd
cd ledger
go mod tidy
go run ./cmd/ledger
cd ..\gateway
go mod tidy
go run .
```

`ledger/cmd/ledger` проверяет подключения и наличие таблиц, выводит сообщение
готовности и завершает работу. Gateway затем открывает собственные подключения
через `ledger.Open` и обслуживает HTTP. Оставьте это окно работающим.
При старте должны появиться `PostgreSQL connected` и `Redis connected`.
Остановка Gateway: `Ctrl+C`.

## 4. Ручная проверка HTTP в Windows CMD

Откройте второе окно CMD, выполните `chcp 65001`.
Ниже `curl.exe` использует правила кавычек **CMD**, а не PowerShell.
Копируйте каждую команду целиком в одну строку. Пример рассчитан на отсутствие
старых расходов в категории «еда»; повторные POST сохраняют новые расходы.

Создать/обновить бюджет — 201:

```cmd
curl.exe -i -X POST http://localhost:8080/api/budgets -H "Content-Type: application/json" -d "{\"category\":\"еда\",\"limit\":5000}"
```

Создать расход — 201:

```cmd
curl.exe -i -X POST http://localhost:8080/api/transactions -H "Content-Type: application/json" -d "{\"amount\":4500,\"category\":\"еда\",\"description\":\"ланч\",\"date\":\"2025-09-10\"}"
```

Превысить суммарный лимит — 409, без записи в expenses:

```cmd
curl.exe -i -X POST http://localhost:8080/api/transactions -H "Content-Type: application/json" -d "{\"amount\":501,\"category\":\"еда\",\"description\":\"кофе\",\"date\":\"2025-09-10\"}"
```

Проверить списки из PostgreSQL:

```cmd
curl.exe -i http://localhost:8080/api/budgets
curl.exe -i http://localhost:8080/api/transactions
```

Вызвать отчёт дважды подряд; URL с `&` обязательно заключить в кавычки:

```cmd
curl.exe -i "http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30"
curl.exe -i "http://localhost:8080/api/reports/summary?from=2025-09-01&to=2025-09-30"
```

Формат отчёта:

```json
{"from":"2025-09-01","to":"2025-09-30","total":4500,"categories":[{"category":"еда","amount":4500}]}
```

Границы интервала включены. При пустом отчёте `total: 0`, `categories: []`.
Первый запрос для нового ключа пишет `report cache miss` в консоль Gateway,
второй в течение 30 секунд — `report cache hit`. После истечения TTL снова miss.
Ответы одинаковы: кеш прозрачен для HTTP. После нового расхода отчёт может
оставаться прежним до истечения 30 секунд; списки всегда читаются из БД.
Если Redis становится недоступен после старта, отчёт вычисляется через БД,
ошибка кеша только логируется.

Можно проверить ключ через Redis CLI в Docker:

```cmd
docker compose exec redis redis-cli TTL report:summary:2025-09-01:2025-09-30
```

Проверка записей в таблицах:

```cmd
docker compose exec postgres psql -U postgres -d cashapp -c "SELECT * FROM budgets;"
docker compose exec postgres psql -U postgres -d cashapp -c "SELECT * FROM expenses ORDER BY id;"
```

Остановите Gateway через Ctrl+C, повторно запустите `go run .` в папке gateway
и выполните GET списков. Данные должны сохраниться. Для проверки после перезапуска
самого PostgreSQL используйте `docker compose restart postgres`, дождитесь
готовности и перезапустите Gateway.

## Контракты и правила

| Метод | Путь | Успех |
| --- | --- | --- |
| POST | /api/budgets | 201, category + limit |
| GET | /api/budgets | 200, массив бюджетов |
| POST | /api/transactions | 201, amount + category + description + date |
| GET | /api/transactions | 200, массив транзакций |
| GET | /api/reports/summary?from=YYYY-MM-DD&to=YYYY-MM-DD | 200, отчёт |

Пустые списки возвращаются как `[]`. Все ответы имеют
`Content-Type: application/json; charset=utf-8`.
Ошибки: 400 — JSON/валидация; 409 — `{"error":"budget exceeded"}`;
500 — `{"error":"internal server error"}`; 404/405 — неизвестный путь/метод.
Формат ошибок единый: `{"error":"описание"}`.

Категории без бюджета разрешены. Лимит действует на всю историю категории,
а не на период отчёта. Достижение лимита разрешено. Проверка суммы и INSERT
выполняются в одной SQL-транзакции с advisory lock категории; SetBudget
использует ту же блокировку, включая случаи отсутствия бюджета.

Amount и Limit положительны, помещаются в NUMERIC(14,2) и имеют не более двух
знаков после запятой. Непустая date проверяется как YYYY-MM-DD; пропущенная
или пустая дата становится текущим днём UTC. ID генерируется PostgreSQL и
читается доменной моделью, но не добавляется в старые HTTP DTO.

В Gateway изменена только необходимая связка: инициализация SQL Ledger,
обработка ошибок List* и новый маршрут отчёта, которого ранее не было.
Прежние четыре пути и JSON-поля сохранены.

## Тесты и покрытие

```cmd
powershell.exe -NoProfile -File scripts\test-coverage.ps1
```

Скрипт выполняет go mod tidy и тесты обоих модулей, создаёт cover.out/cover.html
и обновляет проценты ниже. Отдельный запуск, например из папки ledger:

```cmd
go test ./... -v -cover -coverprofile cover.out
go tool cover -func cover.out
go tool cover -html cover.out -o cover.html
```

Unit-тесты используют MemoryLedger, sqlmock и отдельный miniredis для TTL,
cache hit/miss и повреждённого кеша. Для реального PostgreSQL есть отдельный
интеграционный тест с временной схемой. Подготовьте отдельную БД для тестов:

```cmd
docker compose exec postgres createdb -U postgres cashapp_test
set "TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/cashapp_test?sslmode=disable"
cd ledger
go test -tags integration -v ./...
```

Роль тестовой БД должна иметь право CREATE SCHEMA. Тест сам создаёт временную
схему и удаляет только её; проверяет лимит при параллельных записях, отчёт
и сохранность данных после закрытия/повторного открытия подключения.
SQL Ledger не имеет Reset: очистка рабочей БД из тестов не выполняется.
MemoryLedger.Reset оставлен для изолированных unit-тестов.

<!-- coverage:start -->
Покрытие новой версии пока не измерено: в окружении подготовки нет Go,
PostgreSQL, Redis и Docker. Прежние проценты относятся к версии в памяти.
<!-- coverage:end -->

## Документация

- [Ledger](ledger/README.md): SQL, миграции, конфигурация и кеш.
- [Gateway](gateway/README.md): запуск и проверка HTTP.
- [Goose SQL](https://pressly.github.io/goose/documentation/annotations/)
- [pgx database/sql](https://pkg.go.dev/github.com/jackc/pgx/v5/stdlib)
- [go-redis](https://redis.io/docs/latest/develop/clients/go/)
