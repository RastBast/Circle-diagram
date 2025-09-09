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

type SetSpeedsRequest struct {
	MapID  int       `json:"map_id"`
	Speeds []float64 `json:"speeds"`
}

type NewEpochRequest struct {
	MapID int `json:"map_id"`
}

var db *sql.DB

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

	// Выполняем миграцию
	err = migrateDB()
	if err != nil {
		return err
	}

	// Создаем таблицу для состояний клеток
	createCellsTableSQL := `
	CREATE TABLE IF NOT EXISTS map_cells (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		map_id INTEGER NOT NULL,
		x INTEGER NOT NULL,
		y INTEGER NOT NULL,
		values TEXT NOT NULL,
		FOREIGN KEY(map_id) REFERENCES maps(id)
	);`

	_, err = db.Exec(createCellsTableSQL)
	return err
}

func migrateDB() error {
	// Проверяем существование колонки speeds
	var speedsCount int
	err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('maps') WHERE name='speeds'`).Scan(&speedsCount)
	if err != nil {
		// Если pragma не работает, пробуем альтернативный способ
		_, err = db.Exec("SELECT speeds FROM maps LIMIT 1")
		if err != nil && strings.Contains(err.Error(), "no such column") {
			_, err = db.Exec("ALTER TABLE maps ADD COLUMN speeds TEXT DEFAULT ''")
			if err != nil {
				return fmt.Errorf("ошибка добавления колонки speeds: %v", err)
			}
		}
	} else if speedsCount == 0 {
		_, err = db.Exec("ALTER TABLE maps ADD COLUMN speeds TEXT DEFAULT ''")
		if err != nil {
			return fmt.Errorf("ошибка добавления колонки speeds: %v", err)
		}
	}

	// Проверяем существование колонки epoch
	var epochCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('maps') WHERE name='epoch'`).Scan(&epochCount)
	if err != nil {
		// Если pragma не работает, пробуем альтернативный способ
		_, err = db.Exec("SELECT epoch FROM maps LIMIT 1")
		if err != nil && strings.Contains(err.Error(), "no such column") {
			_, err = db.Exec("ALTER TABLE maps ADD COLUMN epoch INTEGER DEFAULT 0")
			if err != nil {
				return fmt.Errorf("ошибка добавления колонки epoch: %v", err)
			}
		}
	} else if epochCount == 0 {
		_, err = db.Exec("ALTER TABLE maps ADD COLUMN epoch INTEGER DEFAULT 0")
		if err != nil {
			return fmt.Errorf("ошибка добавления колонки epoch: %v", err)
		}
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

// Функции для работы с клетками в БД
func saveCellsToDB(mapID int, cells []Cell) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Удаляем старые данные
	_, err = tx.Exec("DELETE FROM map_cells WHERE map_id = ?", mapID)
	if err != nil {
		return err
	}

	// Сохраняем новые данные
	stmt, err := tx.Prepare("INSERT INTO map_cells (map_id, x, y, values) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, cell := range cells {
		if len(cell.Vals) > 0 {
			valsJSON, _ := json.Marshal(cell.Vals)
			_, err = stmt.Exec(mapID, cell.X, cell.Y, string(valsJSON))
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func loadCellsFromDB(mapID int) ([]Cell, error) {
	rows, err := db.Query("SELECT x, y, values FROM map_cells WHERE map_id = ?", mapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cells := []Cell{}
	for rows.Next() {
		var x, y int
		var valsJSON string
		err = rows.Scan(&x, &y, &valsJSON)
		if err != nil {
			return nil, err
		}

		var vals []int
		err = json.Unmarshal([]byte(valsJSON), &vals)
		if err != nil {
			return nil, err
		}

		cells = append(cells, Cell{X: x, Y: y, Vals: vals})
	}

	return cells, nil
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
		http.Error(w, "Ошибка сохранения скоростей: "+err.Error(), http.StatusInternalServerError)
		return
	}

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

	var cfgStr, circlesStr, speedsStr string
	var epoch int
	err := db.QueryRow("SELECT config, circles, COALESCE(speeds, ''), COALESCE(epoch, 0) FROM maps WHERE id = ?", 
		req.MapID).Scan(&cfgStr, &circlesStr, &speedsStr, &epoch)
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
	var speeds []float64

	if err := json.Unmarshal([]byte(cfgStr), &cfg); err != nil {
		http.Error(w, "Ошибка парсинга config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.Unmarshal([]byte(circlesStr), &circles); err != nil {
		http.Error(w, "Ошибка парсинга circles: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if speedsStr != "" && speedsStr != "[]" {
		if err := json.Unmarshal([]byte(speedsStr), &speeds); err != nil {
			http.Error(w, "Ошибка парсинга speeds: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Получаем текущие клетки из БД
	cells, err := loadCellsFromDB(req.MapID)
	if err != nil {
		http.Error(w, "Ошибка загрузки клеток: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Если клеток нет, генерируем начальное распределение
	if len(cells) == 0 {
		cells = generateDistribution(cfg, circles, []float64{90.0, 10.0})
	}

	// Применяем движение, если есть скорости
	if len(speeds) > 0 {
		cells = moveNumbers(cfg, circles, cells, speeds)
	}

	// Увеличиваем эпоху
	epoch++
	_, err = db.Exec("UPDATE maps SET epoch = ? WHERE id = ?", epoch, req.MapID)
	if err != nil {
		http.Error(w, "Ошибка обновления эпохи: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Сохраняем новое состояние клеток
	if err := saveCellsToDB(req.MapID, cells); err != nil {
		http.Error(w, "Ошибка сохранения клеток: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := struct {
		MapID int    `json:"map_id"`
		Epoch int    `json:"epoch"`
		Cells []Cell `json:"cells"`
	}{req.MapID, epoch, cells}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
	log.Printf("%s %s", r.Method, r.URL.Path)

	switch {
	case r.URL.Path == "/api/maps" && r.Method == http.MethodPost:
		createMapHandler(w, r)
	case r.URL.Path == "/api/distribute" && r.Method == http.MethodPost:
		distributeHandler(w, r)
	case r.URL.Path == "/api/speeds" && r.Method == http.MethodPost:
		setSpeedsHandler(w, r)
	case r.URL.Path == "/api/newEpoch" && r.Method == http.MethodPost:
		newEpochHandler(w, r)
	default:
		http.Error(w, "Endpoint не найден", http.StatusNotFound)
	}
}

func main() {
	log.Println("Инициализация базы данных...")
	if err := initDB(); err != nil {
		log.Fatalf("Ошибка инициализации БД: %v", err)
	}
	defer db.Close()

	http.HandleFunc("/api/", apiHandler)

	log.Println("Сервер запущен на порту :8080")
	log.Println("Доступные endpoints:")
	log.Println(" POST /api/maps - создание карты")
	log.Println(" POST /api/distribute - распределение чисел")
	log.Println(" POST /api/speeds - установка скоростей")
	log.Println(" POST /api/newEpoch - переключение эпохи")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
