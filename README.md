# 1. Запустить сервер
go run main.go

# 2. Создать карту
curl -X POST http://localhost:8080/api/maps \
  -H "Content-Type: application/json" \
  -d '{
    "name": "игровая_карта",
    "config": {
      "width": 15, "height": 15,
      "spawn_count": 2, "bedroom_count": 1,
      "spawn_radius": 2, "bedroom_radius": 1,
      "max_gap": 3
    }
  }'

# 3. Распределить числа (используйте map_id из ответа)
curl -X POST http://localhost:8080/api/distribute \
  -H "Content-Type: application/json" \
  -d '{"map_id": 1, "probabilities": [20, 30, 50]}'

# 4. Создать игрока
curl -X POST http://localhost:8080/api/player/spawn \
  -H "Content-Type: application/json" \
  -d '{"map_id": 1, "name": "Тестер"}'

# 5. Получить обзор игрока (используйте player_id из ответа)
curl -X GET http://localhost:8080/api/player/1/view

# 6. Переместить игрока
curl -X POST http://localhost:8080/api/player/1/move \
  -H "Content-Type: application/json" \
  -d '{"direction": "right"}'
