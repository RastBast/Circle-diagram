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
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Circle struct {
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Radius int    `json:"radius"`
	Type   string `json:"type"` // "spawn" или "bedroom"
}

type MapConfig struct {
	Width         int `json:"width"`
	Height        int `json:"height"`
	SpawnCount    int `json:"spawn_count"`
	BedroomCount  int `json:"bedroom_count"`
	SpawnRadius   int `json:"spawn_radius"`
	BedroomRadius int `json:"bedroom_radius"`
	MaxGap        int `json:"max_gap"`
}

type SavedMap struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Config    MapConfig `json:"config"`
	Circles   []Circle  `json:"circles"`
	CreatedAt time.Time `json:"created_at"`
}

type MapData struct {
	config   MapConfig
	spawns   []Circle
	bedrooms []Circle
}

var db *sql.DB

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./maps.db")
	if err != nil {
		return err
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS maps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		config TEXT NOT NULL,
		circles TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(createTableSQL)
	return err
}

func NewMapData(cfg MapConfig) *MapData {
	return &MapData{
		config:   cfg,
		spawns:   make([]Circle, 0, cfg.SpawnCount),
		bedrooms: make([]Circle, 0, cfg.BedroomCount),
	}
}

func (m *MapData) getAllCircles() []Circle {
	all := make([]Circle, 0, len(m.spawns)+len(m.bedrooms))
	for _, c := range m.spawns {
		circle := c
		circle.Type = "spawn"
		all = append(all, circle)
	}
	for _, c := range m.bedrooms {
		circle := c
		circle.Type = "bedroom"
		all = append(all, circle)
	}
	return all
}

func distance(c1, c2 Circle) float64 {
	dx := float64(c1.X - c2.X)
	dy := float64(c1.Y - c2.Y)
	return math.Sqrt(dx*dx + dy*dy)
}

func circlesOverlap(c1, c2 Circle) bool {
	return distance(c1, c2) < float64(c1.Radius+c2.Radius)
}

func (m *MapData) canPlace(c Circle) bool {
	if c.X-c.Radius < 0 || c.X+c.Radius >= m.config.Width ||
		c.Y-c.Radius < 0 || c.Y+c.Radius >= m.config.Height {
		return false
	}

	for _, existing := range m.getAllCircles() {
		if circlesOverlap(c, existing) {
			return false
		}
	}
	return true
}

func (m *MapData) findClosest(target Circle) float64 {
	minDist := math.Inf(1)

	for _, c := range m.getAllCircles() {
		if c.X == target.X && c.Y == target.Y && c.Radius == target.Radius {
			continue
		}

		dist := distance(target, c)
		if dist < minDist {
			minDist = dist
		}
	}

	return minDist
}

func (m *MapData) generateNearbyPos(baseCircle Circle, newRadius int) (int, int) {
	for attempts := 0; attempts < 100; attempts++ {
		angle := rand.Float64() * 2 * math.Pi
		minDist := float64(baseCircle.Radius + newRadius)
		maxDist := minDist + float64(m.config.MaxGap)
		dist := minDist + rand.Float64()*(maxDist-minDist)

		x := int(float64(baseCircle.X) + dist*math.Cos(angle))
		y := int(float64(baseCircle.Y) + dist*math.Sin(angle))

		if x >= newRadius && x < m.config.Width-newRadius &&
			y >= newRadius && y < m.config.Height-newRadius {
			return x, y
		}
	}

	x := newRadius + rand.Intn(m.config.Width-2*newRadius)
	y := newRadius + rand.Intn(m.config.Height-2*newRadius)
	return x, y
}

func (m *MapData) generate() error {
	rand.Seed(time.Now().UnixNano())

	if m.config.SpawnCount > 0 {
		center := Circle{
			X:      m.config.Width / 2,
			Y:      m.config.Height / 2,
			Radius: m.config.SpawnRadius,
		}

		if m.canPlace(center) {
			m.spawns = append(m.spawns, center)
		}
	}

	for i := len(m.spawns); i < m.config.SpawnCount; i++ {
		placed := false

		for attempts := 0; attempts < 10000; attempts++ {
			var x, y int

			existing := m.getAllCircles()
			if len(existing) > 0 {
				base := existing[rand.Intn(len(existing))]
				x, y = m.generateNearbyPos(base, m.config.SpawnRadius)
			} else {
				x = m.config.SpawnRadius + rand.Intn(m.config.Width-2*m.config.SpawnRadius)
				y = m.config.SpawnRadius + rand.Intn(m.config.Height-2*m.config.SpawnRadius)
			}

			c := Circle{X: x, Y: y, Radius: m.config.SpawnRadius}

			if m.canPlace(c) {
				m.spawns = append(m.spawns, c)
				placed = true
				break
			}
		}

		if !placed {
			return fmt.Errorf("cant place spawn %d", i+1)
		}
	}

	for i := 0; i < m.config.BedroomCount; i++ {
		placed := false

		for attempts := 0; attempts < 10000; attempts++ {
			var x, y int

			existing := m.getAllCircles()
			if len(existing) > 0 {
				base := existing[rand.Intn(len(existing))]
				x, y = m.generateNearbyPos(base, m.config.BedroomRadius)
			} else {
				x = m.config.BedroomRadius + rand.Intn(m.config.Width-2*m.config.BedroomRadius)
				y = m.config.BedroomRadius + rand.Intn(m.config.Height-2*m.config.BedroomRadius)
			}

			c := Circle{X: x, Y: y, Radius: m.config.BedroomRadius}

			if m.canPlace(c) {
				m.bedrooms = append(m.bedrooms, c)
				placed = true
				break
			}
		}

		if !placed {
			return fmt.Errorf("cant place bedroom %d", i+1)
		}
	}

	all := m.getAllCircles()
	if len(all) > 1 {
		violationCount := 0
		for _, c := range all {
			dist := m.findClosest(c)
			if dist > float64(m.config.MaxGap) {
				violationCount++
			}
		}
		if violationCount > 0 {
			log.Printf("Maxgap violations: %d/%d circles", violationCount, len(all))
		}
	}

	return nil
}

func renderMap(config MapConfig, circles []Circle) *image.RGBA {
	cellSize := 100
	maxImageSize := 10000

	if config.Width*cellSize > maxImageSize || config.Height*cellSize > maxImageSize {
		cellSize = maxImageSize / max(config.Width, config.Height)
		if cellSize < 1 {
			cellSize = 1
		}
	}

	w := config.Width * cellSize
	h := config.Height * cellSize

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	white := color.RGBA{255, 255, 255, 255}
	gray := color.RGBA{200, 200, 200, 255}
	blue := color.RGBA{0, 0, 255, 255}
	green := color.RGBA{0, 255, 0, 255}

	// Белый фон
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, white)
		}
	}

	// Сетка
	if cellSize >= 5 {
		for i := 0; i <= config.Width; i++ {
			x := i * cellSize
			for y := 0; y < h; y++ {
				if x < w {
					img.Set(x, y, gray)
				}
			}
		}
		for i := 0; i <= config.Height; i++ {
			y := i * cellSize
			for x := 0; x < w; x++ {
				if y < h {
					img.Set(x, y, gray)
				}
			}
		}
	}

	// Круги
	for _, c := range circles {
		for dy := -c.Radius; dy <= c.Radius; dy++ {
			for dx := -c.Radius; dx <= c.Radius; dx++ {
				if dx*dx+dy*dy <= c.Radius*c.Radius {
					cellX := c.X + dx
					cellY := c.Y + dy

					if cellX >= 0 && cellX < config.Width &&
						cellY >= 0 && cellY < config.Height {

						startX := cellX * cellSize
						startY := cellY * cellSize
						for py := 0; py < cellSize; py++ {
							for px := 0; px < cellSize; px++ {
								imgX := startX + px
								imgY := startY + py
								if imgX < w && imgY < h {
									img.Set(imgX, imgY, blue)
								}
							}
						}
					}
				}
			}
		}

		// Центр
		if c.X >= 0 && c.X < config.Width && c.Y >= 0 && c.Y < config.Height {
			startX := c.X * cellSize
			startY := c.Y * cellSize
			for py := 0; py < cellSize; py++ {
				for px := 0; px < cellSize; px++ {
					imgX := startX + px
					imgY := startY + py
					if imgX < w && imgY < h {
						img.Set(imgX, imgY, green)
					}
				}
			}
		}
	}

	// Перерисовываем сетку
	if cellSize >= 5 {
		for i := 0; i <= config.Width; i++ {
			x := i * cellSize
			for y := 0; y < h; y++ {
				if x < w {
					img.Set(x, y, gray)
				}
			}
		}
		for i := 0; i <= config.Height; i++ {
			y := i * cellSize
			for x := 0; x < w; x++ {
				if y < h {
					img.Set(x, y, gray)
				}
			}
		}
	}

	return img
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// CRUD API handlers

// POST /api/maps - создать новую карту
func createMapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name   string    `json:"name"`
		Config MapConfig `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = fmt.Sprintf("Map_%d", time.Now().Unix())
	}

	// Генерируем карту
	mapData := NewMapData(req.Config)
	if err := mapData.generate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	circles := mapData.getAllCircles()

	// Сохраняем в базу
	configJSON, _ := json.Marshal(req.Config)
	circlesJSON, _ := json.Marshal(circles)

	result, err := db.Exec(
		"INSERT INTO maps (name, config, circles) VALUES (?, ?, ?)",
		req.Name, string(configJSON), string(circlesJSON))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	savedMap := SavedMap{
		ID:        int(id),
		Name:      req.Name,
		Config:    req.Config,
		Circles:   circles,
		CreatedAt: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(savedMap)
}

// GET /api/maps - получить список всех карт
func getMapsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query("SELECT id, name, config, created_at FROM maps ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var maps []struct {
		ID        int       `json:"id"`
		Name      string    `json:"name"`
		Config    MapConfig `json:"config"`
		CreatedAt time.Time `json:"created_at"`
	}

	for rows.Next() {
		var id int
		var name string
		var configJSON string
		var createdAt time.Time

		err := rows.Scan(&id, &name, &configJSON, &createdAt)
		if err != nil {
			continue
		}

		var config MapConfig
		json.Unmarshal([]byte(configJSON), &config)

		maps = append(maps, struct {
			ID        int       `json:"id"`
			Name      string    `json:"name"`
			Config    MapConfig `json:"config"`
			CreatedAt time.Time `json:"created_at"`
		}{
			ID:        id,
			Name:      name,
			Config:    config,
			CreatedAt: createdAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(maps)
}

// GET /api/maps/{id} - получить конкретную карту
func getMapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Path[len("/api/maps/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var name, configJSON, circlesJSON string
	var createdAt time.Time

	err = db.QueryRow("SELECT name, config, circles, created_at FROM maps WHERE id = ?", id).
		Scan(&name, &configJSON, &circlesJSON, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Map not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var config MapConfig
	var circles []Circle
	json.Unmarshal([]byte(configJSON), &config)
	json.Unmarshal([]byte(circlesJSON), &circles)

	savedMap := SavedMap{
		ID:        id,
		Name:      name,
		Config:    config,
		Circles:   circles,
		CreatedAt: createdAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(savedMap)
}

// GET /api/maps/{id}/image - получить изображение карты
func getMapImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Path[len("/api/maps/"):]
	idStr = idStr[:len(idStr)-len("/image")]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var configJSON, circlesJSON string
	err = db.QueryRow("SELECT config, circles FROM maps WHERE id = ?", id).
		Scan(&configJSON, &circlesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Map not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var config MapConfig
	var circles []Circle
	json.Unmarshal([]byte(configJSON), &config)
	json.Unmarshal([]byte(circlesJSON), &circles)

	img := renderMap(config, circles)

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

// DELETE /api/maps/{id} - удалить карту
func deleteMapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Path[len("/api/maps/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM maps WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "Map not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Роутер для API
func apiRouter(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/maps" && r.Method == "POST":
		createMapHandler(w, r)
	case r.URL.Path == "/api/maps" && r.Method == "GET":
		getMapsHandler(w, r)
	case len(r.URL.Path) > len("/api/maps/") && r.URL.Path[:len("/api/maps/")] == "/api/maps/":
		if r.URL.Path[len(r.URL.Path)-6:] == "/image" {
			getMapImageHandler(w, r)
		} else {
			if r.Method == "GET" {
				getMapHandler(w, r)
			} else if r.Method == "DELETE" {
				deleteMapHandler(w, r)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
		}
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// Legacy endpoint для обратной совместимости
func legacyMapHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	width, _ := strconv.Atoi(q.Get("width"))
	height, _ := strconv.Atoi(q.Get("height"))
	spawns, _ := strconv.Atoi(q.Get("spawnscnt"))
	bedrooms, _ := strconv.Atoi(q.Get("bedroomcnt"))
	spawnR, _ := strconv.Atoi(q.Get("spawnradius"))
	bedroomR, _ := strconv.Atoi(q.Get("bedroomradius"))
	maxgap, _ := strconv.Atoi(q.Get("maxgap"))

	config := MapConfig{
		Width:         width,
		Height:        height,
		SpawnCount:    spawns,
		BedroomCount:  bedrooms,
		SpawnRadius:   spawnR,
		BedroomRadius: bedroomR,
		MaxGap:        maxgap,
	}

	mapData := NewMapData(config)
	if err := mapData.generate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	circles := mapData.getAllCircles()
	img := renderMap(config, circles)

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

func main() {
	if err := initDB(); err != nil {
		log.Fatal("Failed to init DB:", err)
	}
	defer db.Close()

	http.HandleFunc("/api/", apiRouter)
	http.HandleFunc("/map", legacyMapHandler) // старый endpoint

	log.Println("Server on :8080")
	log.Println("API endpoints:")
	log.Println("  POST   /api/maps              - create new map")
	log.Println("  GET    /api/maps              - list all maps")
	log.Println("  GET    /api/maps/{id}         - get specific map")
	log.Println("  GET    /api/maps/{id}/image   - get map image")
	log.Println("  DELETE /api/maps/{id}         - delete map")
	log.Println("Legacy:")
	log.Println("  GET    /map?params...         - generate image directly")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
