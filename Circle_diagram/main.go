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

type Cell struct {
	X    int   `json:"x"`
	Y    int   `json:"y"`
	Vals []int `json:"indices"`
}

type CreateDistributionReq struct {
	MapID         int       `json:"map_id"`
	Name          string    `json:"name"`
	Probabilities []float64 `json:"probabilities"`
}

type SavedDistribution struct {
	ID            int       `json:"id"`
	MapID         int       `json:"map_id"`
	Name          string    `json:"name"`
	Probabilities []float64 `json:"probabilities"`
	Cells         []Cell    `json:"cells"`
	Created       time.Time `json:"created_at"`
}

var db *sql.DB

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./maps.db")
	if err != nil {
		return err
	}

	// Таблица карт
	sql := `CREATE TABLE IF NOT EXISTS maps (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		config TEXT NOT NULL,
		circles TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = db.Exec(sql)
	if err != nil {
		return err
	}

	// Таблица распределений
	distSQL := `CREATE TABLE IF NOT EXISTS distributions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		map_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		probabilities TEXT NOT NULL,
		cells TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (map_id) REFERENCES maps(id) ON DELETE CASCADE
	);`

	_, err = db.Exec(distSQL)
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
		cnt := int(prob * 50)
		for j := 0; j < cnt; j++ {
			sel = append(sel, i)
		}
	}

	return sel
}

func distribute(cfg Config, circles []Circle, probs []float64) []Cell {
	var result []Cell
	sel := makeSelector(probs)

	if len(sel) == 0 {
		return result
	}

	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			ct := cellType(x, y, circles)
			var vals []int

			switch ct {
			case 2: // green
				vals = []int{0}
			case 1: // blue
				idx := sel[rand.Intn(len(sel))]
				vals = []int{idx}
			case 0: // white
				count := 1 + rand.Intn(2)
				vals = make([]int, count)
				for i := 0; i < count; i++ {
					vals[i] = sel[rand.Intn(len(sel))]
				}
			}

			if len(vals) > 0 {
				result = append(result, Cell{
					X:    x,
					Y:    y,
					Vals: vals,
				})
			}
		}
	}

	return result
}

// Bitmap шрифт для цифр
func getDigitBitmap(digit rune) [][]bool {
	bitmaps := map[rune][][]bool{
		'0': {
			{false, true, true, true, false},
			{true, false, false, false, true},
			{true, false, false, false, true},
			{true, false, false, false, true},
			{false, true, true, true, false},
		},
		'1': {
			{false, false, true, false, false},
			{false, true, true, false, false},
			{false, false, true, false, false},
			{false, false, true, false, false},
			{false, true, true, true, false},
		},
		'2': {
			{false, true, true, true, false},
			{true, false, false, false, true},
			{false, false, true, true, false},
			{false, true, true, false, false},
			{true, true, true, true, true},
		},
		'3': {
			{true, true, true, true, false},
			{false, false, false, false, true},
			{false, true, true, true, false},
			{false, false, false, false, true},
			{true, true, true, true, false},
		},
		'4': {
			{true, false, false, true, false},
			{true, false, false, true, false},
			{true, true, true, true, true},
			{false, false, false, true, false},
			{false, false, false, true, false},
		},
		'5': {
			{true, true, true, true, true},
			{true, false, false, false, false},
			{true, true, true, true, false},
			{false, false, false, false, true},
			{true, true, true, true, false},
		},
		'6': {
			{false, true, true, true, false},
			{true, false, false, false, false},
			{true, true, true, true, false},
			{true, false, false, false, true},
			{false, true, true, true, false},
		},
		'7': {
			{true, true, true, true, true},
			{false, false, false, false, true},
			{false, false, false, true, false},
			{false, false, true, false, false},
			{false, true, false, false, false},
		},
		'8': {
			{false, true, true, true, false},
			{true, false, false, false, true},
			{false, true, true, true, false},
			{true, false, false, false, true},
			{false, true, true, true, false},
		},
		'9': {
			{false, true, true, true, false},
			{true, false, false, false, true},
			{false, true, true, true, true},
			{false, false, false, false, true},
			{false, true, true, true, false},
		},
		',': {
			{false, false, false, false, false},
			{false, false, false, false, false},
			{false, false, false, false, false},
			{false, false, true, false, false},
			{false, true, false, false, false},
		},
	}

	if bitmap, exists := bitmaps[digit]; exists {
		return bitmap
	}
	return nil
}

func drawText(img *image.RGBA, x, y, cellSize int, text string) {
	black := color.RGBA{0, 0, 0, 255}

	digitW, digitH := 5, 5
	scale := cellSize / 15
	if scale < 1 {
		scale = 1
	}

	startX := x + (cellSize-len(text)*digitW*scale)/2
	startY := y + (cellSize-digitH*scale)/2

	for i, char := range text {
		bitmap := getDigitBitmap(char)
		if bitmap != nil {
			for py := 0; py < digitH; py++ {
				for px := 0; px < digitW; px++ {
					if bitmap[py][px] {
						for sy := 0; sy < scale; sy++ {
							for sx := 0; sx < scale; sx++ {
								imgX := startX + i*digitW*scale + px*scale + sx
								imgY := startY + py*scale + sy
								if imgX >= x && imgX < x+cellSize && imgY >= y && imgY < y+cellSize {
									img.Set(imgX, imgY, black)
								}
							}
						}
					}
				}
			}
		}
	}
}

func renderWithIndices(cfg Config, circles []Circle, cells []Cell) *image.RGBA {
	cs := 100 // Фиксированный размер клетки
	maxSz := 8000

	if cfg.Width*cs > maxSz || cfg.Height*cs > maxSz {
		cs = maxSz / max(cfg.Width, cfg.Height)
		if cs < 20 {
			cs = 20 // Минимум для читаемости
		}
	}

	w := cfg.Width * cs
	h := cfg.Height * cs
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	white := color.RGBA{255, 255, 255, 255}
	gray := color.RGBA{128, 128, 128, 255}
	blue := color.RGBA{0, 0, 255, 255}
	green := color.RGBA{0, 255, 0, 255}

	// Белый фон
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, white)
		}
	}

	// Сетка
	for i := 0; i <= cfg.Width; i++ {
		x := i * cs
		for y := 0; y < h && x < w; y++ {
			img.Set(x, y, gray)
		}
	}
	for i := 0; i <= cfg.Height; i++ {
		y := i * cs
		for x := 0; x < w && y < h; x++ {
			img.Set(x, y, gray)
		}
	}

	// Круги
	for _, c := range circles {
		for dy := -c.Radius; dy <= c.Radius; dy++ {
			for dx := -c.Radius; dx <= c.Radius; dx++ {
				if dx*dx+dy*dy <= c.Radius*c.Radius {
					cellX := c.X + dx
					cellY := c.Y + dy

					if cellX >= 0 && cellX < cfg.Width &&
						cellY >= 0 && cellY < cfg.Height {

						startX := cellX * cs
						startY := cellY * cs

						cellColor := blue
						if dx == 0 && dy == 0 {
							cellColor = green
						}

						for py := 1; py < cs-1; py++ {
							for px := 1; px < cs-1; px++ {
								imgX := startX + px
								imgY := startY + py
								if imgX < w && imgY < h {
									img.Set(imgX, imgY, cellColor)
								}
							}
						}
					}
				}
			}
		}
	}

	// Создаем карту индексов
	cellMap := make(map[string][]int)
	for _, cell := range cells {
		key := fmt.Sprintf("%d,%d", cell.X, cell.Y)
		cellMap[key] = cell.Vals
	}

	// Рисуем цифры
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			key := fmt.Sprintf("%d,%d", x, y)
			if indices, exists := cellMap[key]; exists && len(indices) > 0 {
				var text string
				for i, idx := range indices {
					if i > 0 {
						text += ","
					}
					text += fmt.Sprintf("%d", idx)
				}

				cellX := x * cs
				cellY := y * cs
				drawText(img, cellX, cellY, cs, text)
			}
		}
	}

	return img
}

func render(cfg Config, circles []Circle) *image.RGBA {
	cs := 50
	maxSz := 5000

	if cfg.Width*cs > maxSz || cfg.Height*cs > maxSz {
		cs = maxSz / max(cfg.Width, cfg.Height)
		if cs < 1 {
			cs = 1
		}
	}

	w := cfg.Width * cs
	h := cfg.Height * cs
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	white := color.RGBA{255, 255, 255, 255}
	gray := color.RGBA{128, 128, 128, 255}
	blue := color.RGBA{0, 0, 255, 255}
	green := color.RGBA{0, 255, 0, 255}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, white)
		}
	}

	if cs >= 2 {
		for i := 0; i <= cfg.Width; i++ {
			x := i * cs
			for y := 0; y < h && x < w; y++ {
				img.Set(x, y, gray)
			}
		}
		for i := 0; i <= cfg.Height; i++ {
			y := i * cs
			for x := 0; x < w && y < h; x++ {
				img.Set(x, y, gray)
			}
		}
	}

	for _, c := range circles {
		for dy := -c.Radius; dy <= c.Radius; dy++ {
			for dx := -c.Radius; dx <= c.Radius; dx++ {
				if dx*dx+dy*dy <= c.Radius*c.Radius {
					cellX := c.X + dx
					cellY := c.Y + dy

					if cellX >= 0 && cellX < cfg.Width &&
						cellY >= 0 && cellY < cfg.Height {

						startX := cellX * cs
						startY := cellY * cs
						for py := 0; py < cs; py++ {
							for px := 0; px < cs; px++ {
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

		if c.X >= 0 && c.X < cfg.Width && c.Y >= 0 && c.Y < cfg.Height {
			startX := c.X * cs
			startY := c.Y * cs
			for py := 0; py < cs; py++ {
				for px := 0; px < cs; px++ {
					imgX := startX + px
					imgY := startY + py
					if imgX < w && imgY < h {
						img.Set(imgX, imgY, green)
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

func createDistributionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "bad method", 405)
		return
	}

	var req CreateDistributionReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "bad json", 400)
		return
	}

	if req.Name == "" {
		req.Name = fmt.Sprintf("dist_%d", time.Now().Unix())
	}

	var cfgJSON, circlesJSON string
	err := db.QueryRow("SELECT config, circles FROM maps WHERE id = ?", req.MapID).
		Scan(&cfgJSON, &circlesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "map not found", 404)
		} else {
			http.Error(w, err.Error(), 500)
		}
		return
	}

	var cfg Config
	var circles []Circle
	json.Unmarshal([]byte(cfgJSON), &cfg)
	json.Unmarshal([]byte(circlesJSON), &circles)

	rand.Seed(time.Now().UnixNano())
	cells := distribute(cfg, circles, req.Probabilities)

	probsJSON, _ := json.Marshal(req.Probabilities)
	cellsJSON, _ := json.Marshal(cells)

	result, err := db.Exec(
		"INSERT INTO distributions (map_id, name, probabilities, cells) VALUES (?, ?, ?, ?)",
		req.MapID, req.Name, string(probsJSON), string(cellsJSON))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	id, _ := result.LastInsertId()

	resp := SavedDistribution{
		ID:            int(id),
		MapID:         req.MapID,
		Name:          req.Name,
		Probabilities: req.Probabilities,
		Cells:         cells,
		Created:       time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func getDistributionImageHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/api/distributions/"):]
	idStr = idStr[:len(idStr)-6]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}

	var mapID int
	var cfgJSON, circlesJSON, cellsJSON string

	err = db.QueryRow(`
		SELECT d.map_id, m.config, m.circles, d.cells 
		FROM distributions d 
		JOIN maps m ON d.map_id = m.id 
		WHERE d.id = ?`, id).
		Scan(&mapID, &cfgJSON, &circlesJSON, &cellsJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "not found", 404)
		} else {
			http.Error(w, err.Error(), 500)
		}
		return
	}

	var cfg Config
	var circles []Circle
	var cells []Cell
	json.Unmarshal([]byte(cfgJSON), &cfg)
	json.Unmarshal([]byte(circlesJSON), &circles)
	json.Unmarshal([]byte(cellsJSON), &cells)

	img := renderWithIndices(cfg, circles, cells)

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name, config, created_at FROM maps ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var maps []struct {
		ID      int       `json:"id"`
		Name    string    `json:"name"`
		Config  Config    `json:"config"`
		Created time.Time `json:"created_at"`
	}

	for rows.Next() {
		var id int
		var name, cfgJSON string
		var created time.Time

		if rows.Scan(&id, &name, &cfgJSON, &created) != nil {
			continue
		}

		var cfg Config
		json.Unmarshal([]byte(cfgJSON), &cfg)

		maps = append(maps, struct {
			ID      int       `json:"id"`
			Name    string    `json:"name"`
			Config  Config    `json:"config"`
			Created time.Time `json:"created_at"`
		}{id, name, cfg, created})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(maps)
}

func imageHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/api/maps/"):]
	idStr = idStr[:len(idStr)-6]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}

	var cfgJSON, circlesJSON string
	err = db.QueryRow("SELECT config, circles FROM maps WHERE id = ?", id).
		Scan(&cfgJSON, &circlesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "not found", 404)
		} else {
			http.Error(w, err.Error(), 500)
		}
		return
	}

	var cfg Config
	var circles []Circle
	json.Unmarshal([]byte(cfgJSON), &cfg)
	json.Unmarshal([]byte(circlesJSON), &circles)

	img := render(cfg, circles)

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/maps" && r.Method == "POST":
		createHandler(w, r)
	case r.URL.Path == "/api/maps" && r.Method == "GET":
		listHandler(w, r)
	case r.URL.Path == "/api/distributions" && r.Method == "POST":
		createDistributionHandler(w, r)
	case len(r.URL.Path) > 18 && r.URL.Path[:18] == "/api/distributions":
		if len(r.URL.Path) > 6 && r.URL.Path[len(r.URL.Path)-6:] == "/image" {
			getDistributionImageHandler(w, r)
		}
	case len(r.URL.Path) > 10 && r.URL.Path[:10] == "/api/maps/":
		if len(r.URL.Path) > 6 && r.URL.Path[len(r.URL.Path)-6:] == "/image" {
			imageHandler(w, r)
		}
	default:
		http.Error(w, "not found", 404)
	}
}

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
	img := render(cfg, circles)

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

func main() {
	if err := initDB(); err != nil {
		log.Fatal("DB error:", err)
	}
	defer db.Close()

	http.HandleFunc("/api/", apiHandler)
	http.HandleFunc("/map", legacyHandler)

	log.Println("Server on :8080")
	log.Println("NEW: POST /api/distributions - create distribution with indices")
	log.Println("NEW: GET /api/distributions/{id}/image - get image with numbers")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
