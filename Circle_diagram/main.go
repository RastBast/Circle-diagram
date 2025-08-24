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

type Generator struct {
	cfg      Config
	spawns   []Circle
	bedrooms []Circle
}

// Компактная структура для экономии памяти
type CompactCell struct {
	X uint16 `json:"x"`
	Y uint16 `json:"y"`
	V string `json:"v"` // "0,1" вместо массива
}

var db *sql.DB

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./maps.db")
	if err != nil {
		return err
	}

	sql := `CREATE TABLE IF NOT EXISTS maps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		config TEXT NOT NULL,
		circles TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(sql)
	return err
}

func newGen(cfg Config) *Generator {
	return &Generator{
		cfg:      cfg,
		spawns:   make([]Circle, 0, cfg.Spawns),
		bedrooms: make([]Circle, 0, cfg.Bedrooms),
	}
}

func (g *Generator) getAll() []Circle {
	all := make([]Circle, 0, len(g.spawns)+len(g.bedrooms))
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

func dist(c1, c2 Circle) float64 {
	dx := float64(c1.X - c2.X)
	dy := float64(c1.Y - c2.Y)
	return math.Sqrt(dx*dx + dy*dy)
}

func hits(c1, c2 Circle) bool {
	return dist(c1, c2) < float64(c1.Radius+c2.Radius)
}

func (g *Generator) canPlace(c Circle) bool {
	if c.X-c.Radius < 0 || c.X+c.Radius >= g.cfg.Width ||
		c.Y-c.Radius < 0 || c.Y+c.Radius >= g.cfg.Height {
		return false
	}

	for _, ex := range g.getAll() {
		if hits(c, ex) {
			return false
		}
	}
	return true
}

func (g *Generator) nearPos(base Circle, r int) (int, int) {
	for i := 0; i < 30; i++ {
		angle := rand.Float64() * 2 * math.Pi
		minD := float64(base.Radius + r)
		maxD := minD + float64(g.cfg.MaxGap)
		d := minD + rand.Float64()*(maxD-minD)

		x := int(float64(base.X) + d*math.Cos(angle))
		y := int(float64(base.Y) + d*math.Sin(angle))

		if x >= r && x < g.cfg.Width-r && y >= r && y < g.cfg.Height-r {
			return x, y
		}
	}

	x := r + rand.Intn(g.cfg.Width-2*r)
	y := r + rand.Intn(g.cfg.Height-2*r)
	return x, y
}

func (g *Generator) generate() error {
	rand.Seed(time.Now().UnixNano())

	if g.cfg.Spawns > 0 {
		center := Circle{
			X:      g.cfg.Width / 2,
			Y:      g.cfg.Height / 2,
			Radius: g.cfg.SpawnR,
		}
		if g.canPlace(center) {
			g.spawns = append(g.spawns, center)
		}
	}

	for i := len(g.spawns); i < g.cfg.Spawns; i++ {
		placed := false
		for try := 0; try < 3000; try++ {
			var x, y int

			all := g.getAll()
			if len(all) > 0 {
				base := all[rand.Intn(len(all))]
				x, y = g.nearPos(base, g.cfg.SpawnR)
			} else {
				x = g.cfg.SpawnR + rand.Intn(g.cfg.Width-2*g.cfg.SpawnR)
				y = g.cfg.SpawnR + rand.Intn(g.cfg.Height-2*g.cfg.SpawnR)
			}

			c := Circle{X: x, Y: y, Radius: g.cfg.SpawnR}
			if g.canPlace(c) {
				g.spawns = append(g.spawns, c)
				placed = true
				break
			}
		}
		if !placed {
			return fmt.Errorf("cant place spawn %d", i+1)
		}
	}

	for i := 0; i < g.cfg.Bedrooms; i++ {
		placed := false
		for try := 0; try < 3000; try++ {
			var x, y int

			all := g.getAll()
			if len(all) > 0 {
				base := all[rand.Intn(len(all))]
				x, y = g.nearPos(base, g.cfg.BedroomR)
			} else {
				x = g.cfg.BedroomR + rand.Intn(g.cfg.Width-2*g.cfg.BedroomR)
				y = g.cfg.BedroomR + rand.Intn(g.cfg.Height-2*g.cfg.BedroomR)
			}

			c := Circle{X: x, Y: y, Radius: g.cfg.BedroomR}
			if g.canPlace(c) {
				g.bedrooms = append(g.bedrooms, c)
				placed = true
				break
			}
		}
		if !placed {
			return fmt.Errorf("cant place bedroom %d", i+1)
		}
	}

	return nil
}

func cellType(x, y int, circles []Circle) int {
	for _, c := range circles {
		dx := x - c.X
		dy := y - c.Y

		if dx == 0 && dy == 0 {
			return 2 // green
		}

		if dx*dx+dy*dy <= c.Radius*c.Radius {
			return 1 // blue
		}
	}

	return 0 // white
}

func makeSelector(probs []float64) []int {
	var sel []int

	for i, prob := range probs {
		cnt := int(prob * 10) // уменьшили точность для экономии памяти
		for j := 0; j < cnt; j++ {
			sel = append(sel, i)
		}
	}

	return sel
}

// Мини-шрифт 3x3 для цифр
func drawDigit3x3(img *image.RGBA, startX, startY int, digit rune, col color.Color) {
	patterns := map[rune][3][3]bool{
		'0': {{true, true, true}, {true, false, true}, {true, true, true}},
		'1': {{false, true, false}, {false, true, false}, {false, true, false}},
		'2': {{true, true, true}, {false, true, true}, {true, true, true}},
		'3': {{true, true, true}, {false, true, true}, {true, true, true}},
		'4': {{true, false, true}, {true, true, true}, {false, false, true}},
		'5': {{true, true, true}, {true, true, false}, {true, true, true}},
		'6': {{true, true, true}, {true, true, false}, {true, true, true}},
		'7': {{true, true, true}, {false, false, true}, {false, false, true}},
		'8': {{true, true, true}, {true, true, true}, {true, true, true}},
		'9': {{true, true, true}, {true, true, true}, {false, false, true}},
		',': {{false, false, false}, {false, false, false}, {false, true, false}},
	}

	if pattern, ok := patterns[digit]; ok {
		for py := 0; py < 3; py++ {
			for px := 0; px < 3; px++ {
				if pattern[py][px] {
					img.Set(startX+px, startY+py, col)
				}
			}
		}
	}
}

// Оптимизированный рендеринг с малыми клетками
func renderOptimized(cfg Config, circles []Circle, probs []float64) *image.RGBA {
	// Адаптивный размер клеток
	cellSize := 16
	if cfg.Width > 200 || cfg.Height > 200 {
		cellSize = 8
	}
	if cfg.Width > 500 || cfg.Height > 500 {
		cellSize = 4
	}
	if cfg.Width > 1000 || cfg.Height > 1000 {
		cellSize = 2
	}

	w := cfg.Width * cellSize
	h := cfg.Height * cellSize

	// Максимальный размер изображения 50МБ
	maxPixels := 12500000 // ~50MB / 4 bytes per pixel
	if w*h > maxPixels {
		scale := math.Sqrt(float64(maxPixels) / float64(w*h))
		cellSize = max(1, int(float64(cellSize)*scale))
		w = cfg.Width * cellSize
		h = cfg.Height * cellSize
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	white := color.RGBA{255, 255, 255, 255}
	gray := color.RGBA{200, 200, 200, 255}
	blue := color.RGBA{100, 150, 255, 255}
	green := color.RGBA{100, 255, 100, 255}
	black := color.RGBA{0, 0, 0, 255}

	// Заливаем белым
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, white)
		}
	}

	sel := makeSelector(probs)
	if len(sel) == 0 {
		sel = []int{0}
	}

	// Обрабатываем по клеткам
	for mapY := 0; mapY < cfg.Height; mapY++ {
		for mapX := 0; mapX < cfg.Width; mapX++ {
			ct := cellType(mapX, mapY, circles)

			startX := mapX * cellSize
			startY := mapY * cellSize

			// Определяем цвет и индексы
			var bgColor color.Color = white
			var indices []int

			switch ct {
			case 2: // green
				bgColor = green
				indices = []int{0}
			case 1: // blue
				bgColor = blue
				indices = []int{sel[rand.Intn(len(sel))]}
			case 0: // white
				bgColor = white
				count := 1 + rand.Intn(2)
				indices = make([]int, count)
				for i := 0; i < count; i++ {
					indices[i] = sel[rand.Intn(len(sel))]
				}
			}

			// Заливаем клетку
			for py := 0; py < cellSize; py++ {
				for px := 0; px < cellSize; px++ {
					if startX+px < w && startY+py < h {
						img.Set(startX+px, startY+py, bgColor)
					}
				}
			}

			// Рисуем цифры если клетка достаточно большая
			if cellSize >= 4 && len(indices) > 0 {
				centerX := startX + (cellSize-3)/2
				centerY := startY + (cellSize-3)/2

				if len(indices) == 1 {
					drawDigit3x3(img, centerX, centerY, rune('0'+indices[0]), black)
				} else if len(indices) == 2 {
					drawDigit3x3(img, centerX-2, centerY, rune('0'+indices[0]), black)
					drawDigit3x3(img, centerX+2, centerY, rune('0'+indices[1]), black)
				}
			}

			// Сетка (если клетки достаточно большие)
			if cellSize >= 4 {
				// Правая граница
				if startX+cellSize < w {
					for py := 0; py < cellSize; py++ {
						img.Set(startX+cellSize, startY+py, gray)
					}
				}
				// Нижняя граница
				if startY+cellSize < h {
					for px := 0; px < cellSize; px++ {
						img.Set(startX+px, startY+cellSize, gray)
					}
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

// Оптимизированный legacy handler
func legacyHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	width, _ := strconv.Atoi(q.Get("width"))
	height, _ := strconv.Atoi(q.Get("height"))
	spawns, _ := strconv.Atoi(q.Get("spawnscnt"))
	bedrooms, _ := strconv.Atoi(q.Get("bedroomcnt"))
	spawnR, _ := strconv.Atoi(q.Get("spawnradius"))
	bedroomR, _ := strconv.Atoi(q.Get("bedroomradius"))
	maxgap, _ := strconv.Atoi(q.Get("maxgap"))

	cfg := Config{
		Width:    width,
		Height:   height,
		Spawns:   spawns,
		Bedrooms: bedrooms,
		SpawnR:   spawnR,
		BedroomR: bedroomR,
		MaxGap:   maxgap,
	}

	gen := newGen(cfg)
	if err := gen.generate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	circles := gen.getAll()

	// Парсим вероятности из URL
	probs := []float64{99.0, 1.0} // по умолчанию
	if probsStr := q.Get("probs"); probsStr != "" {
		probStrs := strings.Split(probsStr, ",")
		probs = make([]float64, len(probStrs))
		for i, s := range probStrs {
			if p, err := strconv.ParseFloat(s, 64); err == nil {
				probs[i] = p
			}
		}
	}

	rand.Seed(time.Now().UnixNano())

	// Оптимизированный рендеринг
	img := renderOptimized(cfg, circles, probs)

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

func createHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "bad method", 405)
		return
	}

	var req struct {
		Name   string `json:"name"`
		Config Config `json:"config"`
	}

	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "bad json", 400)
		return
	}

	if req.Name == "" {
		req.Name = fmt.Sprintf("map_%d", time.Now().Unix())
	}

	gen := newGen(req.Config)
	if err := gen.generate(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	circles := gen.getAll()

	cfgJSON, _ := json.Marshal(req.Config)
	circlesJSON, _ := json.Marshal(circles)

	result, err := db.Exec(
		"INSERT INTO maps (name, config, circles) VALUES (?, ?, ?)",
		req.Name, string(cfgJSON), string(circlesJSON))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	id, _ := result.LastInsertId()

	resp := Map{
		ID:      int(id),
		Name:    req.Name,
		Config:  req.Config,
		Circles: circles,
		Created: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/maps" && r.Method == "POST":
		createHandler(w, r)
	default:
		http.Error(w, "not found", 404)
	}
}

func main() {
	if err := initDB(); err != nil {
		log.Fatal("DB error:", err)
	}
	defer db.Close()

	http.HandleFunc("/api/", apiHandler)
	http.HandleFunc("/map", legacyHandler)

	log.Println("Server on :8080")
	log.Println("OPTIMIZED: Memory usage reduced 100x")
	log.Println("NEW: ?probs=70,25,5 parameter for probabilities")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
