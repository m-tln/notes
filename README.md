# Note Service

Минималистичный сервис заметок на Go с PostgreSQL и балансировщиком нагрузки.

## Структура проекта
```
note-service/
├── app/                 # Основное приложение
├── loadbalancer/        # Балансировщик на Go
├── docker-compose.yml   # Конфигурация Docker
└── init.sql            # Инициализация БД
```

## Быстрый старт

### 1. Запуск всей системы
```bash
# Собрать и запустить
docker-compose up --build -d

# Проверить статус
docker-compose ps

# Посмотреть логи
docker-compose logs -f { app1 | app2 | app3 | postgres | loadbalancer }
```

### 2. Проверка работы
```bash
# Health check
curl http://localhost:8080/health

# Создать заметку
curl -X POST http://localhost:8080/notes \
  -H "Content-Type: application/json" \
  -d '{"title":"Моя заметка","content":"Текст заметки"}'

# Получить все заметки
curl http://localhost:8080/notes

# Получить конкретную заметку
curl http://localhost:8080/notes/1

# Обновить заметку
curl -X PUT http://localhost:8080/notes/1 \
  -H "Content-Type: application/json" \
  -d '{"title":"Обновлено","content":"Новый текст"}'

# Удалить заметку
curl -X DELETE http://localhost:8080/notes/1
```

## Проверка балансировки

### 1. Создать несколько запросов
```bash
for i in {1..10}; do
  curl -s -X POST http://localhost:8080/notes \
    -H "Content-Type: application/json" \
    -d "{\"title\":\"Note $i\",\"content\":\"Content $i\"}" > /dev/null
  echo "Request $i sent"
done
```

### 2. Проверить распределение по логам
```bash
docker-compose logs app1 | grep "Created note" | wc -l
docker-compose logs app2 | grep "Created note" | wc -l
docker-compose logs app3 | grep "Created note" | wc -l
```

## Тестирование отказоустойчивости

### 1. Остановить один инстанс
```bash
docker-compose stop app2
```

### 2. Проверить что система работает
```bash
curl http://localhost:8080/notes
```

### 3. Посмотреть логи балансировщика
```bash
docker-compose logs loadbalancer | grep -i "down\|up\|error"
```

### 4. Запустить инстанс обратно
```bash
docker-compose start app2

# Подождать health check
sleep 15

# Проверить восстановление
docker-compose logs loadbalancer | grep "back up"
```

## Проверка базы данных

```bash
docker-compose exec postgres psql -U notes_user -d notes_db

# Внутри psql:
SELECT * FROM notes;
\q
```

## Полная перезагрузка
```bash
# Остановить и удалить всё
docker-compose down -v

# Запустить заново
docker-compose up --build -d
```

## Основные endpoints

- `GET /health` - проверка здоровья
- `GET /notes` - получить все заметки
- `POST /notes` - создать заметку
- `GET /notes/:id` - получить заметку по ID
- `PUT /notes/:id` - обновить заметку
- `DELETE /notes/:id` - удалить заметку

## Полезные команды

```bash
# Очистить всё
docker-compose down -v --rmi all

# Просмотр логов в реальном времени
docker-compose logs -f --tail=100
```

# Load Balancer (Go)

Минималистичный round-robin load balancer с health checks и circuit breaker.

## API

- `GET /` - проксирует трафик на бэкенды
- `GET /health` - проверка состояния балансировщика
- `GET /status` - JSON статус всех бэкендов

## Особенности

- Round-robin алгоритм
- Health checks каждые 10 секунд
- Circuit breaker (3 failures → 30s пауза)
- Автоматический retry при ошибках
- Graceful shutdown

## Портфолио

- **PostgreSQL** для хранения данных
- **Кастомный балансировщик** на Go с health checks
- **Docker Compose** для оркестрации
- **Отказоустойчивость** с circuit breaker
- **HTTPS** поддержка
- **Graceful shutdown**