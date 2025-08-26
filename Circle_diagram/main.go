package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Базовые структуры данных
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
	Created time.Time `json:"created_at"`
}

type Cell struct {
	X    int   `json:"x"`
	Y    int   `json:"y"`
	Vals []int `json:"indices"`
}

// Глобальная переменная для БД
var db *sql.DB

// Инициализация базы данных
func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./maps.db")
	if err != nil {
		return err
	}

	createTableSQL := `CREATE TABLE IF NOT EXISTS maps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		config TEXT NOT NULL,
		circles TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(createTableSQL)
	return err
}

// Генератор карт
type MapGenerator struct {
	config   Config
	spawns   []Circle
	bedrooms []Circle
}

func NewMapGenerator(cfg Config) *MapGenerator {
	return &MapGenerator{
		config:   cfg,
		spawns:   make([]Circle, 0),
		bedrooms: make([]Circle, 0),
	}
}

func (g *MapGenerator) getAllCircles() []Circle {
	all := make([]Circle, 0)

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

// Проверка пересечений кругов
func (g *MapGenerator) canPlaceCircle(newCircle Circle) bool {
	// Проверка границ карты
	if newCircle.X-newCircle.Radius < 0 || newCircle.X+newCircle.Radius >= g.config.Width ||
		newCircle.Y-newCircle.Radius < 0 || newCircle.Y+newCircle.Radius >= g.config.Height {
		return false
	}

	// Проверка пересечений с существующими кругами
	for _, existing := range g.getAllCircles() {
		distance := math.Sqrt(float64((newCircle.X-existing.X)*(newCircle.X-existing.X) +
			(newCircle.Y-existing.Y)*(newCircle.Y-existing.Y)))

		if distance < float64(newCircle.Radius+existing.Radius) {
			return false
		}
	}

	return true
}

// Генерация позиции рядом с существующим кругом
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

	// Fallback - случайная позиция
	x := radius + rand.Intn(g.config.Width-2*radius)
	y := radius + rand.Intn(g.config.Height-2*radius)
	return x, y
}

// Основная функция генерации карты
func (g *MapGenerator) Generate() error {
	rand.Seed(time.Now().UnixNano())

	// Размещаем первый spawn в центре
	if g.config.Spawns > 0 {
		centerSpawn := Circle{
			X:      g.config.Width / 2,
			Y:      g.config.Height / 2,
			Radius: g.config.SpawnR,
		}

		if g.canPlaceCircle(centerSpawn) {
			g.spawns = append(g.spawns, centerSpawn)
		}
	}

	// Размещаем остальные spawn круги
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

			newSpawn := Circle{X: x, Y: y, Radius: g.config.SpawnR}
			if g.canPlaceCircle(newSpawn) {
				g.spawns = append(g.spawns, newSpawn)
				placed = true
				break
			}
		}

		if !placed {
			return fmt.Errorf("не удалось разместить spawn %d", i+1)
		}
	}

	// Размещаем bedroom круги
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

			newBedroom := Circle{X: x, Y: y, Radius: g.config.BedroomR}
			if g.canPlaceCircle(newBedroom) {
				g.bedrooms = append(g.bedrooms, newBedroom)
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

// Определение типа клетки
func getCellType(x, y int, circles []Circle) int {
	for _, circle := range circles {
		dx := x - circle.X
		dy := y - circle.Y

		// Центр круга - зеленый
		if dx == 0 && dy == 0 {
			return 2
		}

		// Внутри круга - синий
		if dx*dx+dy*dy <= circle.Radius*circle.Radius {
			return 1
		}
	}

	// Пустая клетка - белая
	return 0
}

// Создание селектора для вероятностей
func createProbabilitySelector(probabilities []float64) []int {
	selector := make([]int, 0)

	for index, probability := range probabilities {
		count := int(probability * 50) // достаточная точность
		for j := 0; j < count; j++ {
			selector = append(selector, index)
		}
	}

	return selector
}

// Простые цифровые паттерны 5x7
var digitPatterns = map[rune][][]bool{
	'0': {
		{true, true, true, true, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, true, true, true, true},
	},
	'1': {
		{false, false, true, false, false},
		{false, true, true, false, false},
		{false, false, true, false, false},
		{false, false, true, false, false},
		{false, false, true, false, false},
		{false, false, true, false, false},
		{true, true, true, true, true},
	},
	'2': {
		{true, true, true, true, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
		{true, true, true, true, true},
		{true, false, false, false, false},
		{true, false, false, false, false},
		{true, true, true, true, true},
	},
	'3': {
		{true, true, true, true, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
		{true, true, true, true, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
		{true, true, true, true, true},
	},
	'4': {
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, true, true, true, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
	},
	'5': {
		{true, true, true, true, true},
		{true, false, false, false, false},
		{true, false, false, false, false},
		{true, true, true, true, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
		{true, true, true, true, true},
	},
	'6': {
		{true, true, true, true, true},
		{true, false, false, false, false},
		{true, false, false, false, false},
		{true, true, true, true, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, true, true, true, true},
	},
	'7': {
		{true, true, true, true, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
		{false, false, false, true, false},
		{false, false, true, false, false},
		{false, true, false, false, false},
		{true, false, false, false, false},
	},
	'8': {
		{true, true, true, true, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, true, true, true, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, true, true, true, true},
	},
	'9': {
		{true, true, true, true, true},
		{true, false, false, false, true},
		{true, false, false, false, true},
		{true, true, true, true, true},
		{false, false, false, false, true},
		{false, false, false, false, true},
		{true, true, true, true, true},
	},
}

// Отрисовка цифры
func drawDigit(img *image.RGBA, x, y int, digit rune, scale int) {
	pattern, exists := digitPatterns[digit]
	if !exists {
		return
	}

	black := color.RGBA{0, 0, 0, 255}

	for row := 0; row < len(pattern); row++ {
		for col := 0; col < len(pattern[row]); col++ {
			if pattern[row][col] {
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						img.Set(x+col*scale+sx, y+row*scale+sy, black)
					}
				}
			}
		}
	}
}

// Рендеринг карты с цифрами
func renderMapWithNumbers(cfg Config, circles []Circle, probabilities []float64) *image.RGBA {
	cellSize := 100

	// Ограничиваем размер карты для экономии памяти
	if cfg.Width > 500 {
		cfg.Width = 500
	}
	if cfg.Height > 500 {
		cfg.Height = 500
	}

	width := cfg.Width * cellSize
	height := cfg.Height * cellSize
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Определяем цвета
	white := color.RGBA{255, 255, 255, 255}
	gray := color.RGBA{128, 128, 128, 255}
	blue := color.RGBA{100, 150, 255, 255}
	green := color.RGBA{100, 255, 100, 255}

	selector := createProbabilitySelector(probabilities)
	if len(selector) == 0 {
		selector = []int{0} // fallback
	}

	// Рендерим каждую клетку
	for mapY := 0; mapY < cfg.Height; mapY++ {
		for mapX := 0; mapX < cfg.Width; mapX++ {
			cellType := getCellType(mapX, mapY, circles)

			startX := mapX * cellSize
			startY := mapY * cellSize

			var backgroundColor color.Color
			var digitValue int

			switch cellType {
			case 2: // зеленая клетка (центр круга)
				backgroundColor = green
				digitValue = 0
			case 1: // синяя клетка (внутри круга)
				backgroundColor = blue
				digitValue = selector[rand.Intn(len(selector))]
			case 0: // белая клетка (пустая)
				backgroundColor = white
				digitValue = selector[rand.Intn(len(selector))]
			}

			// Заливаем клетку цветом
			for py := 0; py < cellSize; py++ {
				for px := 0; px < cellSize; px++ {
					img.Set(startX+px, startY+py, backgroundColor)
				}
			}

			// Рисуем рамку клетки
			for py := 0; py < cellSize; py++ {
				img.Set(startX+cellSize-1, startY+py, gray) // правая
				img.Set(startX, startY+py, gray)            // левая
			}
			for px := 0; px < cellSize; px++ {
				img.Set(startX+px, startY+cellSize-1, gray) // нижняя
				img.Set(startX+px, startY, gray)            // верхняя
			}

			// Рисуем цифру в центре клетки
			if digitValue >= 0 && digitValue <= 9 {
				digitX := startX + (cellSize-5*8)/2 // центрируем цифру 5x7 с масштабом 8
				digitY := startY + (cellSize-7*8)/2
				drawDigit(img, digitX, digitY, rune('0'+digitValue), 8)
			}
		}
	}

	return img
}

// HTTP обработчики
func mapHandler(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	cfg := Config{
		Width:    parseIntParam(params.Get("width")),
		Height:   parseIntParam(params.Get("height")),
		Spawns:   parseIntParam(params.Get("spawnscnt")),
		Bedrooms: parseIntParam(params.Get("bedroomcnt")),
		SpawnR:   parseIntParam(params.Get("spawnradius")),
		BedroomR: parseIntParam(params.Get("bedroomradius")),
		MaxGap:   parseIntParam(params.Get("maxgap")),
	}

	generator := NewMapGenerator(cfg)
	err := generator.Generate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	circles := generator.getAllCircles()

	// Парсим вероятности
	probabilities := []float64{90.0, 10.0} // по умолчанию
	if probsParam := params.Get("probs"); probsParam != "" {
		probabilities = parseProbabilities(probsParam)
	}

	rand.Seed(time.Now().UnixNano())
	img := renderMapWithNumbers(cfg, circles, probabilities)

	w.Header().Set("Content-Type", "image/png")
	err = png.Encode(w, img)
	if err != nil {
		log.Printf("Ошибка кодирования PNG: %v", err)
	}
}

func createMapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		Name   string `json:"name"`
		Config Config `json:"config"`
	}

	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Некорректный JSON", http.StatusBadRequest)
		return
	}

	if request.Name == "" {
		request.Name = fmt.Sprintf("map_%d", time.Now().Unix())
	}

	generator := NewMapGenerator(request.Config)
	err = generator.Generate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	circles := generator.getAllCircles()

	// Сохраняем в базу данных
	configJSON, _ := json.Marshal(request.Config)
	circlesJSON, _ := json.Marshal(circles)

	result, err := db.Exec(
		"INSERT INTO maps (name, config, circles) VALUES (?, ?, ?)",
		request.Name, string(configJSON), string(circlesJSON))
	if err != nil {
		http.Error(w, "Ошибка сохранения в БД", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	response := Map{
		ID:      int(id),
		Name:    request.Name,
		Config:  request.Config,
		Circles: circles,
		Created: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func distributeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		MapID         int       `json:"map_id"`
		Probabilities []float64 `json:"probabilities"`
	}

	err := json.NewDecoder(r.Body).Decode(&request)
	if err != nil {
		http.Error(w, "Некорректный JSON", http.StatusBadRequest)
		return
	}

	// Получаем карту из БД
	var configJSON, circlesJSON string
	err = db.QueryRow("SELECT config, circles FROM maps WHERE id = ?", request.MapID).
		Scan(&configJSON, &circlesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Карта не найдена", http.StatusNotFound)
		} else {
			http.Error(w, "Ошибка БД", http.StatusInternalServerError)
		}
		return
	}

	var config Config
	var circles []Circle
	json.Unmarshal([]byte(configJSON), &config)
	json.Unmarshal([]byte(circlesJSON), &circles)

	// Генерируем распределение
	cells := generateDistribution(config, circles, request.Probabilities)

	response := struct {
		MapID int    `json:"map_id"`
		Cells []Cell `json:"cells"`
	}{request.MapID, cells}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Вспомогательные функции
func parseIntParam(param string) int {
	value, _ := strconv.Atoi(param)
	return value
}

func parseProbabilities(probsParam string) []float64 {
	probStrings := strings.Split(probsParam, ",")
	probabilities := make([]float64, len(probStrings))

	for i, probString := range probStrings {
		prob, err := strconv.ParseFloat(probString, 64)
		if err == nil {
			probabilities[i] = prob
		}
	}

	return probabilities
}

func generateDistribution(cfg Config, circles []Circle, probabilities []float64) []Cell {
	cells := make([]Cell, 0)
	selector := createProbabilitySelector(probabilities)

	if len(selector) == 0 {
		return cells
	}

	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			cellType := getCellType(x, y, circles)
			var values []int

			switch cellType {
			case 2: // зеленая клетка
				values = []int{0}
			case 1: // синяя клетка
				values = []int{selector[rand.Intn(len(selector))]}
			case 0: // белая клетка
				values = []int{selector[rand.Intn(len(selector))]}
			}

			if len(values) > 0 {
				cells = append(cells, Cell{
					X:    x,
					Y:    y,
					Vals: values,
				})
			}
		}
	}

	return cells
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/maps" && r.Method == "POST":
		createMapHandler(w, r)
	case r.URL.Path == "/api/distribute" && r.Method == "POST":
		distributeHandler(w, r)
	default:
		http.Error(w, "Endpoint не найден", http.StatusNotFound)
	}
}

func main() {
	err := initDB()
	if err != nil {
		log.Fatal("Ошибка инициализации БД:", err)
	}
	defer db.Close()

	http.HandleFunc("/api/", apiHandler)
	http.HandleFunc("/map", mapHandler)

	log.Println("Сервер запущен на порту :8080")
	log.Println("Доступные endpoints:")
	log.Println("  GET  /map - генерация карты с цифрами")
	log.Println("  POST /api/maps - создание карты")
	log.Println("  POST /api/distribute - распределение индексов")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
