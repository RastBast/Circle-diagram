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

type DistributeReq struct {
	MapID int       `json:"map_id"`
	Probs []float64 `json:"probabilities"`
}

type Cell struct {
	X    int   `json:"x"`
	Y    int   `json:"y"`
	Vals []int `json:"indices"`
}

type DistributeResp struct {
	MapID int    `json:"map_id"`
	Cells []Cell `json:"cells"`
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

	// center spawn
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

	// other spawns
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

	// bedrooms
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
		cnt := int(prob * 50) // smaller scale
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

func render(cfg Config, circles []Circle) *image.RGBA {
	cs := 50 // smaller cell size
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

	// fill white
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, white)
		}
	}

	// grid
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

	// circles
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

		// center
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

func getHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/api/maps/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}

	var name, cfgJSON, circlesJSON string
	var created time.Time

	err = db.QueryRow("SELECT name, config, circles, created_at FROM maps WHERE id = ?", id).
		Scan(&name, &cfgJSON, &circlesJSON, &created)
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

	resp := Map{
		ID:      id,
		Name:    name,
		Config:  cfg,
		Circles: circles,
		Created: created,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

func deleteHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/api/maps/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}

	result, err := db.Exec("DELETE FROM maps WHERE id = ?", id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "not found", 404)
		return
	}

	w.WriteHeader(204)
}

func distributeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "bad method", 405)
		return
	}

	var req DistributeReq
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "bad json", 400)
		return
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
	cells := distribute(cfg, circles, req.Probs)

	resp := DistributeResp{
		MapID: req.MapID,
		Cells: cells,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/maps" && r.Method == "POST":
		createHandler(w, r)
	case r.URL.Path == "/api/maps" && r.Method == "GET":
		listHandler(w, r)
	case r.URL.Path == "/api/distribute" && r.Method == "POST":
		distributeHandler(w, r)
	case len(r.URL.Path) > 10 && r.URL.Path[:10] == "/api/maps/":
		if len(r.URL.Path) > 6 && r.URL.Path[len(r.URL.Path)-6:] == "/image" {
			imageHandler(w, r)
		} else {
			if r.Method == "GET" {
				getHandler(w, r)
			} else if r.Method == "DELETE" {
				deleteHandler(w, r)
			} else {
				http.Error(w, "bad method", 405)
			}
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
	log.Fatal(http.ListenAndServe(":8080", nil))
}
