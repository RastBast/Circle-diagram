package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Circle struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Radius int    `json:"radius"`
	Type   string `json:"type"`
}

type Config struct {
	Width    int `json:"width"`
	Height   int `json:"height"`
	Spawns   int `json:"spawn_count"`
	Bedrooms int `json:"bedroom_count"`
	SpawnR   int `json:"spawn_radius"`
	BedroomR int `json:"bedroom_radius"`
	MaxGap   int `json:"max_gap"`
}

type Map struct {
	ID      int       `json:"id"`
	Name    string    `json:"name"`
	Config  Config    `json:"config"`
	Circles []Circle  `json:"circles"`
	Speeds  []float64 `json:"speeds,omitempty"`
	Epoch   int       `json:"epoch"`
	Created time.Time `json:"created_at"`
}

type Cell struct {
	X    int   `json:"x"`
	Y    int   `json:"y"`
	Vals []int `json:"indices"`
}

// СТРУКТУРЫ ДЛЯ ИГРОКА
type Player struct {
	ID       int       `json:"id"`
	MapID    int       `json:"map_id"`
	X        int       `json:"x"`
	Y        int       `json:"y"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created_at"`
}

type SpawnPlayerRequest struct {
	MapID int    `json:"map_id"`
	Name  string `json:"name"`
}

type MovePlayerRequest struct {
	Direction string `json:"direction"` // "up", "down", "left", "right"
}

type SetSpeedsRequest struct {
	MapID  int       `json:"map_id"`
	Speeds []float64 `json:"speeds"`
}

type NewEpochRequest struct {
	MapID int `json:"map_id"`
}

var db *sql.DB

func forceMigration() error {
	log.Println("🔧 Принудительная миграция базы данных...")

	// Проверяем существование колонок и добавляем если нужно
	migrations := []struct {
		sql  string
		desc string
	}{
		{"ALTER TABLE maps ADD COLUMN speeds TEXT DEFAULT '';", "speeds"},
		{"ALTER TABLE maps ADD COLUMN epoch INTEGER DEFAULT 0;", "epoch"},
	}

	for i, migration := range migrations {
		_, err := db.Exec(migration.sql)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate column") {
				log.Printf("   ✅ Миграция %d (%s): колонка уже существует", i+1, migration.desc)
			} else {
				log.Printf("   ⚠️  Миграция %d (%s): %v", i+1, migration.desc, err)
			}
		} else {
			log.Printf("   ✅ Миграция %d (%s): колонка добавлена", i+1, migration.desc)
		}
	}

	// Принудительно удаляем и пересоздаем таблицу map_cells
	log.Println("🔧 Пересоздание таблицы map_cells...")
	_, err := db.Exec("DROP TABLE IF EXISTS map_cells;")
	if err != nil {
		log.Printf("   ⚠️  Не удалось удалить старую таблицу: %v", err)
	}

	// Создаем таблицу клеток с правильным синтаксисом
	cellsTable := `CREATE TABLE map_cells (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		map_id INTEGER NOT NULL,
		x INTEGER NOT NULL,
		y INTEGER NOT NULL,
		cell_values TEXT NOT NULL,
		FOREIGN KEY(map_id) REFERENCES maps(id)
	);`

	_, err = db.Exec(cellsTable)
	if err != nil {
		log.Printf("   ❌ Ошибка создания таблицы map_cells: %v", err)
		return err
	} else {
		log.Printf("   ✅ Таблица map_cells создана успешно")
	}

	// ТАБЛИЦА ДЛЯ ИГРОКОВ
	log.Println("🔧 Создание таблицы игроков...")
	playersTable := `CREATE TABLE IF NOT EXISTS players (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		map_id INTEGER NOT NULL,
		x INTEGER NOT NULL,
		y INTEGER NOT NULL,
		name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(map_id) REFERENCES maps(id)
	);`

	_, err = db.Exec(playersTable)
	if err != nil {
		log.Printf("   ❌ Ошибка создания таблицы players: %v", err)
	} else {
		log.Printf("   ✅ Таблица players создана успешно")
	}

	log.Println("🎉 Миграция завершена!")
	return nil
}

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./maps.db")
	if err != nil {
		return err
	}

	// Создаем основную таблицу
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS maps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		config TEXT NOT NULL,
		circles TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		return err
	}

	// Принудительная миграция
	err = forceMigration()
	if err != nil {
		return err
	}

	return nil
}

type MapGenerator struct {
	config   Config
	spawns   []Circle
	bedrooms []Circle
}

func NewMapGenerator(cfg Config) *MapGenerator {
	return &MapGenerator{
		config:   cfg,
		spawns:   []Circle{},
		bedrooms: []Circle{},
	}
}

func (g *MapGenerator) getAllCircles() []Circle {
	all := []Circle{}
	for _, c := range g.spawns {
		c.Type = "spawn"
		all = append(all, c)
	}
	for _, c := range g.bedrooms {
		c.Type = "bedroom"
		all = append(all, c)
	}
	return all
}

func (g *MapGenerator) canPlaceCircle(newCircle Circle) bool {
	if newCircle.X-newCircle.Radius < 0 || newCircle.X+newCircle.Radius >= g.config.Width ||
		newCircle.Y-newCircle.Radius < 0 || newCircle.Y+newCircle.Radius >= g.config.Height {
		return false
	}
	for _, existing := range g.getAllCircles() {
		distance := math.Sqrt(float64((newCircle.X-existing.X)*(newCircle.X-existing.X) +
			(newCircle.Y-existing.Y)*(newCircle.Y-existing.Y)))
		if distance < float64(newCircle.Radius+existing.Radius) {
			return false
		}
	}
	return true
}

func (g *MapGenerator) generateNearbyPosition(baseCircle Circle, radius int) (int, int) {
	for attempts := 0; attempts < 30; attempts++ {
		angle := rand.Float64() * 2 * math.Pi
		minDistance := float64(baseCircle.Radius + radius)
		maxDistance := minDistance + float64(g.config.MaxGap)
		distance := minDistance + rand.Float64()*(maxDistance-minDistance)

		x := int(float64(baseCircle.X) + distance*math.Cos(angle))
		y := int(float64(baseCircle.Y) + distance*math.Sin(angle))

		if x >= radius && x < g.config.Width-radius && y >= radius && y < g.config.Height-radius {
			return x, y
		}
	}
	x := radius + rand.Intn(g.config.Width-2*radius)
	y := radius + rand.Intn(g.config.Height-2*radius)
	return x, y
}

func (g *MapGenerator) Generate() error {
	rand.Seed(time.Now().UnixNano())

	if g.config.Spawns > 0 {
		center := Circle{
			X:      g.config.Width / 2,
			Y:      g.config.Height / 2,
			Radius: g.config.SpawnR,
		}
		if g.canPlaceCircle(center) {
			g.spawns = append(g.spawns, center)
		}
	}

	for i := len(g.spawns); i < g.config.Spawns; i++ {
		placed := false
		for attempts := 0; attempts < 3000; attempts++ {
			var x, y int
			existing := g.getAllCircles()
			if len(existing) > 0 {
				base := existing[rand.Intn(len(existing))]
				x, y = g.generateNearbyPosition(base, g.config.SpawnR)
			} else {
				x = g.config.SpawnR + rand.Intn(g.config.Width-2*g.config.SpawnR)
				y = g.config.SpawnR + rand.Intn(g.config.Height-2*g.config.SpawnR)
			}
			newCircle := Circle{X: x, Y: y, Radius: g.config.SpawnR}
			if g.canPlaceCircle(newCircle) {
				g.spawns = append(g.spawns, newCircle)
				placed = true
				break
			}
		}
		if !placed {
			return fmt.Errorf("не удалось разместить spawn %d", i+1)
		}
	}

	for i := 0; i < g.config.Bedrooms; i++ {
		placed := false
		for attempts := 0; attempts < 3000; attempts++ {
			var x, y int
			existing := g.getAllCircles()
			if len(existing) > 0 {
				base := existing[rand.Intn(len(existing))]
				x, y = g.generateNearbyPosition(base, g.config.BedroomR)
			} else {
				x = g.config.BedroomR + rand.Intn(g.config.Width-2*g.config.BedroomR)
				y = g.config.BedroomR + rand.Intn(g.config.Height-2*g.config.BedroomR)
			}
			newCircle := Circle{X: x, Y: y, Radius: g.config.BedroomR}
			if g.canPlaceCircle(newCircle) {
				g.bedrooms = append(g.bedrooms, newCircle)
				placed = true
				break
			}
		}
		if !placed {
			return fmt.Errorf("не удалось разместить bedroom %d", i+1)
		}
	}
	return nil
}

func getCellType(x, y int, circles []Circle) int {
	for _, circle := range circles {
		dx := x - circle.X
		dy := y - circle.Y

		if dx == 0 && dy == 0 {
			return 2 // зеленая (центр круга)
		}
		if dx*dx+dy*dy <= circle.Radius*circle.Radius {
			return 1 // синяя (внутри круга)
		}
	}
	return 0 // белая (вне кругов)
}

func createProbabilitySelector(probabilities []float64) []int {
	selector := []int{}
	for idx, p := range probabilities {
		count := int(p * 50)
		for i := 0; i < count; i++ {
			selector = append(selector, idx)
		}
	}
	return selector
}

func generateDistribution(cfg Config, circles []Circle, probabilities []float64) []Cell {
	cells := []Cell{}
	selector := createProbabilitySelector(probabilities)
	if len(selector) == 0 {
		return cells
	}

	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			cellType := getCellType(x, y, circles)
			var vals []int
			switch cellType {
			case 2: // зеленая - 0 чисел
				continue
			case 1: // синяя - 1 число
				vals = []int{selector[rand.Intn(len(selector))]}
			case 0: // белая - 1-2 числа
				count := 1 + rand.Intn(2)
				vals = make([]int, count)
				for i := 0; i < count; i++ {
					vals[i] = selector[rand.Intn(len(selector))]
				}
			}
			if len(vals) > 0 {
				cells = append(cells, Cell{X: x, Y: y, Vals: vals})
			}
		}
	}
	return cells
}

func getNeighbors(x, y int, cfg Config) []struct{ X, Y int } {
	directions := []struct{ dx, dy int }{
		{-1, -1}, {-1, 0}, {-1, 1},
		{0, -1}, {0, 1},
		{1, -1}, {1, 0}, {1, 1},
	}
	neighbors := []struct{ X, Y int }{}
	for _, d := range directions {
		nx, ny := x+d.dx, y+d.dy
		if nx >= 0 && nx < cfg.Width && ny >= 0 && ny < cfg.Height {
			neighbors = append(neighbors, struct{ X, Y int }{nx, ny})
		}
	}
	return neighbors
}

func moveNumbers(cfg Config, circles []Circle, cells []Cell, speeds []float64) []Cell {
	if len(speeds) == 0 {
		log.Println("⚠️  Скорости не установлены, числа не двигаются")
		return cells
	}

	rand.Seed(time.Now().UnixNano())

	// Создаем карту текущих позиций
	state := make(map[string][]int)
	for _, cell := range cells {
		key := fmt.Sprintf("%d,%d", cell.X, cell.Y)
		state[key] = append([]int{}, cell.Vals...)
	}

	// Создаем новую карту для результатов
	newState := make(map[string][]int)

	// Инициализируем новую карту пустыми слайсами
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			key := fmt.Sprintf("%d,%d", x, y)
			newState[key] = []int{}
		}
	}

	// Обрабатываем каждую клетку
	for _, cell := range cells {
		for _, val := range cell.Vals {
			speedIdx := val
			if speedIdx >= len(speeds) {
				speedIdx = 0
			}

			speed := speeds[speedIdx]
			if rand.Float64()*100 < speed {
				// Пытаемся переместить число
				moved := false
				neighbors := getNeighbors(cell.X, cell.Y, cfg)

				// Перемешиваем соседей для случайности
				for i := len(neighbors) - 1; i > 0; i-- {
					j := rand.Intn(i + 1)
					neighbors[i], neighbors[j] = neighbors[j], neighbors[i]
				}

				for _, neigh := range neighbors {
					neighborKey := fmt.Sprintf("%d,%d", neigh.X, neigh.Y)
					neighborType := getCellType(neigh.X, neigh.Y, circles)
					currentCount := len(newState[neighborKey])

					canMove := false
					switch neighborType {
					case 0: // белая - максимум 2
						canMove = currentCount < 2
					case 1: // синяя - максимум 1  
						canMove = currentCount < 1
					case 2: // зеленая - недоступна
						canMove = false
					}

					if canMove {
						newState[neighborKey] = append(newState[neighborKey], val)
						moved = true
						break
					}
				}

				if !moved {
					// Число остается на прежнем месте
					cellKey := fmt.Sprintf("%d,%d", cell.X, cell.Y)
					newState[cellKey] = append(newState[cellKey], val)
				}
			} else {
				// Число остается на прежнем месте
				cellKey := fmt.Sprintf("%d,%d", cell.X, cell.Y)
				newState[cellKey] = append(newState[cellKey], val)
			}
		}
	}

	// Преобразуем обратно в Cell slice
	result := []Cell{}
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			key := fmt.Sprintf("%d,%d", x, y)
			if vals := newState[key]; len(vals) > 0 {
				result = append(result, Cell{X: x, Y: y, Vals: vals})
			}
		}
	}
	return result
}

// ФУНКЦИИ ДЛЯ РАБОТЫ С БД
func saveCellsToDB(mapID int, cells []Cell) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("начало транзакции: %v", err)
	}
	defer tx.Rollback()

	// Удаляем старые данные
	_, err = tx.Exec("DELETE FROM map_cells WHERE map_id = ?", mapID)
	if err != nil {
		return fmt.Errorf("удаление старых клеток: %v", err)
	}

	// ИСПРАВЛЕНО: используем cell_values вместо values
	stmt, err := tx.Prepare("INSERT INTO map_cells (map_id, x, y, cell_values) VALUES (?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("подготовка запроса: %v", err)
	}
	defer stmt.Close()

	for _, cell := range cells {
		if len(cell.Vals) > 0 {
			valsJSON, _ := json.Marshal(cell.Vals)
			_, err = stmt.Exec(mapID, cell.X, cell.Y, string(valsJSON))
			if err != nil {
				return fmt.Errorf("вставка клетки (%d,%d): %v", cell.X, cell.Y, err)
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("коммит транзакции: %v", err)
	}

	return nil
}

func loadCellsFromDB(mapID int) ([]Cell, error) {
	// ИСПРАВЛЕНО: используем cell_values и добавлено WHERE условие
	rows, err := db.Query("SELECT x, y, cell_values FROM map_cells WHERE map_id = ?", mapID)
	if err != nil {
		return nil, fmt.Errorf("запрос клеток: %v", err)
	}
	defer rows.Close()

	cells := []Cell{}
	for rows.Next() {
		var x, y int
		var valsJSON string
		err = rows.Scan(&x, &y, &valsJSON)
		if err != nil {
			return nil, fmt.Errorf("чтение строки: %v", err)
		}

		var vals []int
		err = json.Unmarshal([]byte(valsJSON), &vals)
		if err != nil {
			return nil, fmt.Errorf("парсинг values: %v", err)
		}

		cells = append(cells, Cell{X: x, Y: y, Vals: vals})
	}

	return cells, nil
}

// ФУНКЦИИ ДЛЯ ИГРОКОВ
func getSpawnPoints(circles []Circle) []Circle {
	spawns := []Circle{}
	for _, circle := range circles {
		if circle.Type == "spawn" {
			spawns = append(spawns, circle)
		}
	}
	return spawns
}

func getRandomSpawnPoint(spawns []Circle) (int, int) {
	if len(spawns) == 0 {
		return 0, 0 // fallback
	}

	spawn := spawns[rand.Intn(len(spawns))]
	// Случайная позиция внутри spawn круга, но не в центре
	angle := rand.Float64() * 2 * math.Pi
	radius := 1 + rand.Float64()*float64(spawn.Radius-1)

	x := spawn.X + int(radius*math.Cos(angle))
	y := spawn.Y + int(radius*math.Sin(angle))

	return x, y
}

// Валидация данных
func validateSpeeds(speeds []float64) error {
	if len(speeds) == 0 {
		return fmt.Errorf("массив скоростей не может быть пустым")
	}
	for i, speed := range speeds {
		if speed < 0 || speed > 100 {
			return fmt.Errorf("скорость [%d] должна быть от 0 до 100, получено: %f", i, speed)
		}
	}
	return nil
}

func validateConfig(cfg Config) error {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("размеры карты должны быть положительными")
	}
	if cfg.Width > 100 || cfg.Height > 100 {
		return fmt.Errorf("размеры карты слишком большие (max 100x100)")
	}
	if cfg.Spawns < 0 || cfg.Bedrooms < 0 {
		return fmt.Errorf("количество spawn/bedroom не может быть отрицательным")
	}
	return nil
}

// HTTP Handlers

func createMapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name   string `json:"name"`
		Config Config `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateConfig(req.Config); err != nil {
		http.Error(w, "Некорректная конфигурация: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = fmt.Sprintf("map_%d", time.Now().Unix())
	}

	gen := NewMapGenerator(req.Config)
	if err := gen.Generate(); err != nil {
		http.Error(w, "Ошибка генерации: "+err.Error(), http.StatusBadRequest)
		return
	}

	circles := gen.getAllCircles()
	configBytes, _ := json.Marshal(req.Config)
	circlesBytes, _ := json.Marshal(circles)

	res, err := db.Exec("INSERT INTO maps (name, config, circles) VALUES (?, ?, ?)", 
		req.Name, string(configBytes), string(circlesBytes))
	if err != nil {
		http.Error(w, "Ошибка сохранения в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := res.LastInsertId()
	resp := Map{
		ID:      int(id),
		Name:    req.Name,
		Config:  req.Config,
		Circles: circles,
		Epoch:   0,
		Created: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func distributeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		MapID         int       `json:"map_id"`
		Probabilities []float64 `json:"probabilities"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	var configStr, circlesStr string
	err := db.QueryRow("SELECT config, circles FROM maps WHERE id = ?", req.MapID).
		Scan(&configStr, &circlesStr)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Карта не найдена", http.StatusNotFound)
		} else {
			http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var cfg Config
	var circles []Circle
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		http.Error(w, "Ошибка парсинга config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.Unmarshal([]byte(circlesStr), &circles); err != nil {
		http.Error(w, "Ошибка парсинга circles: "+err.Error(), http.StatusInternalServerError)
		return
	}

	cells := generateDistribution(cfg, circles, req.Probabilities)

	// Сохраняем клетки в БД
	if err := saveCellsToDB(req.MapID, cells); err != nil {
		http.Error(w, "Ошибка сохранения клеток: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := struct {
		MapID int    `json:"map_id"`
		Cells []Cell `json:"cells"`
	}{req.MapID, cells}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func setSpeedsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req SetSpeedsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateSpeeds(req.Speeds); err != nil {
		http.Error(w, "Некорректные скорости: "+err.Error(), http.StatusBadRequest)
		return
	}

	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM maps WHERE id = ?", req.MapID).Scan(&exists)
	if err != nil || exists == 0 {
		http.Error(w, "Карта не найдена", http.StatusNotFound)
		return
	}

	speedBytes, _ := json.Marshal(req.Speeds)
	_, err = db.Exec("UPDATE maps SET speeds = ? WHERE id = ?", string(speedBytes), req.MapID)
	if err != nil {
		log.Printf("❌ Ошибка SQL при сохранении скоростей: %v", err)
		http.Error(w, "Ошибка сохранения скоростей: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Скорости сохранены для карты %d", req.MapID)

	resp := struct {
		MapID   int       `json:"map_id"`
		Speeds  []float64 `json:"speeds"`
		Success bool      `json:"success"`
	}{req.MapID, req.Speeds, true}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func newEpochHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req NewEpochRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Получаем данные карты с обработкой NULL значений
	var cfgStr, circlesStr, speedsStr sql.NullString
	var epoch sql.NullInt64
	err := db.QueryRow("SELECT config, circles, speeds, epoch FROM maps WHERE id = ?", 
		req.MapID).Scan(&cfgStr, &circlesStr, &speedsStr, &epoch)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Карта не найдена", http.StatusNotFound)
		} else {
			log.Printf("❌ Ошибка SQL: %v", err)
			http.Error(w, "Ошибка БД при получении карты: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var cfg Config
	var circles []Circle
	var speeds []float64

	if err := json.Unmarshal([]byte(cfgStr.String), &cfg); err != nil {
		http.Error(w, "Ошибка парсинга config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.Unmarshal([]byte(circlesStr.String), &circles); err != nil {
		http.Error(w, "Ошибка парсинга circles: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if speedsStr.Valid && speedsStr.String != "" && speedsStr.String != "[]" {
		if err := json.Unmarshal([]byte(speedsStr.String), &speeds); err != nil {
			http.Error(w, "Ошибка парсинга speeds: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Получаем текущие клетки из БД
	cells, err := loadCellsFromDB(req.MapID)
	if err != nil {
		log.Printf("⚠️  Ошибка загрузки клеток: %v", err)
		http.Error(w, "Ошибка загрузки клеток: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Если клеток нет, генерируем начальное распределение
	if len(cells) == 0 {
		cells = generateDistribution(cfg, circles, []float64{90.0, 10.0})
		log.Printf("📋 Сгенерировано начальное распределение для карты %d", req.MapID)
	}

	// Применяем движение, если есть скорости
	if len(speeds) > 0 {
		cells = moveNumbers(cfg, circles, cells, speeds)
		log.Printf("🎯 Применено движение чисел для карты %d", req.MapID)
	} else {
		log.Printf("⚠️  Скорости не установлены для карты %d, числа не двигаются", req.MapID)
	}

	// Увеличиваем эпоху
	currentEpoch := int(epoch.Int64)
	currentEpoch++
	_, err = db.Exec("UPDATE maps SET epoch = ? WHERE id = ?", currentEpoch, req.MapID)
	if err != nil {
		log.Printf("❌ Ошибка обновления эпохи: %v", err)
		http.Error(w, "Ошибка обновления эпохи: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Сохраняем новое состояние клеток
	if err := saveCellsToDB(req.MapID, cells); err != nil {
		log.Printf("❌ Ошибка сохранения клеток: %v", err)
		http.Error(w, "Ошибка сохранения клеток: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := struct {
		MapID int    `json:"map_id"`
		Epoch int    `json:"epoch"`
		Cells []Cell `json:"cells"`
	}{req.MapID, currentEpoch, cells}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HANDLERS ДЛЯ ИГРОКОВ

func spawnPlayerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req SpawnPlayerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = fmt.Sprintf("Игрок_%d", time.Now().Unix())
	}

	// Получаем карту и её круги
	var circlesStr string
	err := db.QueryRow("SELECT circles FROM maps WHERE id = ?", req.MapID).Scan(&circlesStr)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Карта не найдена", http.StatusNotFound)
		} else {
			http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var circles []Circle
	if err := json.Unmarshal([]byte(circlesStr), &circles); err != nil {
		http.Error(w, "Ошибка парсинга circles: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Получаем точки спавна
	spawns := getSpawnPoints(circles)
	if len(spawns) == 0 {
		http.Error(w, "На карте нет точек спавна", http.StatusBadRequest)
		return
	}

	// Выбираем случайную позицию спавна
	spawnX, spawnY := getRandomSpawnPoint(spawns)

	// Создаем игрока в БД
	res, err := db.Exec("INSERT INTO players (map_id, x, y, name) VALUES (?, ?, ?, ?)", 
		req.MapID, spawnX, spawnY, req.Name)
	if err != nil {
		http.Error(w, "Ошибка создания игрока: "+err.Error(), http.StatusInternalServerError)
		return
	}

	playerID, _ := res.LastInsertId()

	player := Player{
		ID:      int(playerID),
		MapID:   req.MapID,
		X:       spawnX,
		Y:       spawnY,
		Name:    req.Name,
		Created: time.Now(),
	}

	log.Printf("🎮 Игрок %s создан на карте %d в позиции (%d, %d)", req.Name, req.MapID, spawnX, spawnY)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(player)
}

func movePlayerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Извлекаем player_id из URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		http.Error(w, "Некорректный URL", http.StatusBadRequest)
		return
	}

	playerID, err := strconv.Atoi(pathParts[3])
	if err != nil {
		http.Error(w, "Некорректный ID игрока", http.StatusBadRequest)
		return
	}

	var req MovePlayerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Получаем текущую позицию игрока и данные карты
	var currentX, currentY, mapID int
	err = db.QueryRow("SELECT x, y, map_id FROM players WHERE id = ?", playerID).
		Scan(&currentX, &currentY, &mapID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Игрок не найден", http.StatusNotFound)
		} else {
			http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Получаем размеры карты
	var configStr string
	err = db.QueryRow("SELECT config FROM maps WHERE id = ?", mapID).Scan(&configStr)
	if err != nil {
		http.Error(w, "Ошибка получения карты: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var cfg Config
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		http.Error(w, "Ошибка парсинга config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Вычисляем новую позицию
	newX, newY := currentX, currentY
	switch req.Direction {
	case "up":
		newY = currentY - 1
	case "down":
		newY = currentY + 1
	case "left":
		newX = currentX - 1
	case "right":
		newX = currentX + 1
	default:
		http.Error(w, "Некорректное направление. Используйте: up, down, left, right", http.StatusBadRequest)
		return
	}

	// Проверяем границы карты
	if newX < 0 || newX >= cfg.Width || newY < 0 || newY >= cfg.Height {
		http.Error(w, "Выход за границы карты", http.StatusBadRequest)
		return
	}

	// Обновляем позицию игрока в БД
	_, err = db.Exec("UPDATE players SET x = ?, y = ? WHERE id = ?", newX, newY, playerID)
	if err != nil {
		http.Error(w, "Ошибка обновления позиции: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("🎮 Игрок %d переместился с (%d, %d) на (%d, %d)", playerID, currentX, currentY, newX, newY)

	resp := struct {
		PlayerID int    `json:"player_id"`
		X        int    `json:"x"`
		Y        int    `json:"y"`
		Message  string `json:"message"`
	}{playerID, newX, newY, fmt.Sprintf("Игрок перемещен %s на (%d, %d)", req.Direction, newX, newY)}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// JSON-версия обзора игрока (БЕЗ внешних зависимостей)
func playerViewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Извлекаем player_id из URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		http.Error(w, "Некорректный URL", http.StatusBadRequest)
		return
	}

	playerID, err := strconv.Atoi(pathParts[3])
	if err != nil {
		http.Error(w, "Некорректный ID игрока", http.StatusBadRequest)
		return
	}

	// Получаем данные игрока
	var playerX, playerY, mapID int
	var playerName string
	err = db.QueryRow("SELECT x, y, map_id, name FROM players WHERE id = ?", playerID).
		Scan(&playerX, &playerY, &mapID, &playerName)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Игрок не найден", http.StatusNotFound)
		} else {
			http.Error(w, "Ошибка БД: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Получаем данные карты
	var configStr, circlesStr string
	err = db.QueryRow("SELECT config, circles FROM maps WHERE id = ?", mapID).
		Scan(&configStr, &circlesStr)
	if err != nil {
		http.Error(w, "Ошибка получения карты: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var cfg Config
	var circles []Circle
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		http.Error(w, "Ошибка парсинга config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.Unmarshal([]byte(circlesStr), &circles); err != nil {
		http.Error(w, "Ошибка парсинга circles: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Получаем текущие клетки
	cells, err := loadCellsFromDB(mapID)
	if err != nil {
		http.Error(w, "Ошибка загрузки клеток: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Создаем карту клеток для быстрого поиска
	cellMap := make(map[string][]int)
	for _, cell := range cells {
		key := fmt.Sprintf("%d,%d", cell.X, cell.Y)
		cellMap[key] = cell.Vals
	}

	// Создаем JSON обзор 5x5
	viewData := make([][]map[string]interface{}, 5)
	for i := range viewData {
		viewData[i] = make([]map[string]interface{}, 5)
	}

	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			mapX := playerX + dx
			mapY := playerY + dy

			viewX := dx + 2
			viewY := dy + 2

			cellData := map[string]interface{}{
				"map_x": mapX,
				"map_y": mapY,
				"view_x": viewX,
				"view_y": viewY,
				"is_player": dx == 0 && dy == 0,
			}

			if mapX < 0 || mapX >= cfg.Width || mapY < 0 || mapY >= cfg.Height {
				// Вне карты
				cellData["type"] = "outside"
				cellData["color"] = "black" 
				cellData["description"] = "Вне карты"
				cellData["numbers"] = []int{}
			} else {
				cellType := getCellType(mapX, mapY, circles)
				switch cellType {
				case 0: // белая
					cellData["type"] = "empty"
					cellData["color"] = "white"
					cellData["description"] = "Пустая клетка"
				case 1: // синяя
					cellData["type"] = "inside_circle" 
					cellData["color"] = "blue"
					cellData["description"] = "Внутри круга"
				case 2: // зеленая
					cellData["type"] = "circle_center"
					cellData["color"] = "green"
					cellData["description"] = "Центр круга"
				}

				// Добавляем числа в клетке
				key := fmt.Sprintf("%d,%d", mapX, mapY)
				if numbers, exists := cellMap[key]; exists {
					cellData["numbers"] = numbers
				} else {
					cellData["numbers"] = []int{}
				}
			}

			viewData[viewY][viewX] = cellData
		}
	}

	response := map[string]interface{}{
		"player_id":   playerID,
		"player_name": playerName,
		"player_pos":  map[string]int{"x": playerX, "y": playerY},
		"map_id":      mapID,
		"view_size":   "5x5",
		"view_grid":   viewData,
		"legend": map[string]string{
			"white": "Пустая клетка (можно разместить 1-2 числа)",
			"blue":  "Внутри круга (можно разместить 1 число)", 
			"green": "Центр круга (числа не размещаются)",
			"black": "Вне карты (недоступно)",
		},
		"description": "Обзор игрока 5x5 с центром в его позиции. is_player=true отмечает клетку игрока",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("🎮 Создан JSON-обзор для игрока %d (%s) в позиции (%d, %d)", playerID, playerName, playerX, playerY)
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	// Добавляем CORS заголовки
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		return
	}

	// Логируем запросы
	log.Printf("📡 %s %s", r.Method, r.URL.Path)

	switch {
	case r.URL.Path == "/api/maps" && r.Method == http.MethodPost:
		createMapHandler(w, r)
	case r.URL.Path == "/api/distribute" && r.Method == http.MethodPost:
		distributeHandler(w, r)
	case r.URL.Path == "/api/speeds" && r.Method == http.MethodPost:
		setSpeedsHandler(w, r)
	case r.URL.Path == "/api/newEpoch" && r.Method == http.MethodPost:
		newEpochHandler(w, r)

	// ЭНДПОИНТЫ ДЛЯ ИГРОКОВ
	case r.URL.Path == "/api/player/spawn" && r.Method == http.MethodPost:
		spawnPlayerHandler(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/player/") && strings.HasSuffix(r.URL.Path, "/move") && r.Method == http.MethodPost:
		movePlayerHandler(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/player/") && strings.HasSuffix(r.URL.Path, "/view") && r.Method == http.MethodGet:
		playerViewHandler(w, r)

	default:
		http.Error(w, "Endpoint не найден", http.StatusNotFound)
	}
}

func main() {
	log.Println("🚀 Запуск Circle-diagram сервера с поддержкой игроков (JSON версия)...")
	log.Println("📊 Инициализация базы данных...")
	if err := initDB(); err != nil {
		log.Fatalf("❌ Ошибка инициализации БД: %v", err)
	}
	defer db.Close()

	http.HandleFunc("/api/", apiHandler)

	log.Println("✅ Сервер запущен на порту :8080")
	log.Println("📋 Доступные endpoints:")
	log.Println("   POST /api/maps - создание карты")
	log.Println("   POST /api/distribute - распределение чисел")
	log.Println("   POST /api/speeds - установка скоростей")
	log.Println("   POST /api/newEpoch - переключение эпохи")
	log.Println("🎮 ENDPOINTS ДЛЯ ИГРОКОВ:")
	log.Println("   POST /api/player/spawn - создание игрока")
	log.Println("   POST /api/player/{id}/move - перемещение игрока")
	log.Println("   GET  /api/player/{id}/view - обзор игрока (JSON)")
	log.Println("🎮 Готов к игре!")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
