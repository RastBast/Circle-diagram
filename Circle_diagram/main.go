package main

import (
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
)

type Circle struct {
	X      int
	Y      int
	Radius int
}

type MapConfig struct {
	Width         int
	Height        int
	SpawnCount    int
	BedroomCount  int
	SpawnRadius   int
	BedroomRadius int
	MaxGap        int
}

type MapData struct {
	config   MapConfig
	spawns   []Circle
	bedrooms []Circle
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
	all = append(all, m.spawns...)
	all = append(all, m.bedrooms...)
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

func (m *MapData) generate() error {
	rand.Seed(time.Now().UnixNano())

	// Place first spawn in center
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

	// Place remaining spawns
	for i := len(m.spawns); i < m.config.SpawnCount; i++ {
		placed := false

		for attempts := 0; attempts < 5000; attempts++ {
			var x, y int

			existing := m.getAllCircles()
			if len(existing) > 0 {
				base := existing[rand.Intn(len(existing))]
				angle := rand.Float64() * 2 * math.Pi
				dist := float64(base.Radius + m.config.SpawnRadius + 1 + rand.Intn(50))

				x = int(float64(base.X) + dist*math.Cos(angle))
				y = int(float64(base.Y) + dist*math.Sin(angle))
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

	// Place bedrooms
	for i := 0; i < m.config.BedroomCount; i++ {
		placed := false

		for attempts := 0; attempts < 5000; attempts++ {
			var x, y int

			existing := m.getAllCircles()
			if len(existing) > 0 {
				base := existing[rand.Intn(len(existing))]
				angle := rand.Float64() * 2 * math.Pi
				dist := float64(base.Radius + m.config.BedroomRadius + 1 + rand.Intn(50))

				x = int(float64(base.X) + dist*math.Cos(angle))
				y = int(float64(base.Y) + dist*math.Sin(angle))
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

	// Check maxgap but dont fail, just warn
	all := m.getAllCircles()
	if len(all) > 1 {
		for _, c := range all {
			dist := m.findClosest(c)
			if dist > float64(m.config.MaxGap) {
				log.Printf("Warning: circle at (%d,%d) gap=%.1f > %d", 
					c.X, c.Y, dist, m.config.MaxGap)
			}
		}
	}

	return nil
}

func (m *MapData) render() *image.RGBA {
	cellSize := 1
	if m.config.Width <= 100 && m.config.Height <= 100 {
		cellSize = 10
	}

	w := m.config.Width * cellSize
	h := m.config.Height * cellSize

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// White bg
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, white)
		}
	}

	// Grid for small maps
	if cellSize > 1 {
		gray := color.RGBA{200, 200, 200, 255}
		for i := 0; i <= m.config.Width; i++ {
			x := i * cellSize
			for y := 0; y < h; y++ {
				if x < w {
					img.Set(x, y, gray)
				}
			}
		}
		for i := 0; i <= m.config.Height; i++ {
			y := i * cellSize
			for x := 0; x < w; x++ {
				if y < h {
					img.Set(x, y, gray)
				}
			}
		}
	}

	blue := color.RGBA{0, 0, 255, 255}
	green := color.RGBA{0, 255, 0, 255}

	// Draw circles
	for _, c := range m.getAllCircles() {
		for dy := -c.Radius; dy <= c.Radius; dy++ {
			for dx := -c.Radius; dx <= c.Radius; dx++ {
				if dx*dx+dy*dy <= c.Radius*c.Radius {
					cellX := c.X + dx
					cellY := c.Y + dy

					if cellX >= 0 && cellX < m.config.Width && 
					   cellY >= 0 && cellY < m.config.Height {

						for py := 0; py < cellSize; py++ {
							for px := 0; px < cellSize; px++ {
								imgX := cellX*cellSize + px
								imgY := cellY*cellSize + py
								if imgX < w && imgY < h {
									img.Set(imgX, imgY, blue)
								}
							}
						}
					}
				}
			}
		}

		// Center green
		if c.X >= 0 && c.X < m.config.Width && c.Y >= 0 && c.Y < m.config.Height {
			for py := 0; py < cellSize; py++ {
				for px := 0; px < cellSize; px++ {
					imgX := c.X*cellSize + px
					imgY := c.Y*cellSize + py
					if imgX < w && imgY < h {
						img.Set(imgX, imgY, green)
					}
				}
			}
		}
	}

	return img
}

func parseRequest(r *http.Request) (*MapConfig, error) {
	q := r.URL.Query()

	width, err := strconv.Atoi(q.Get("width"))
	if err != nil || width <= 0 || width > 2000 {
		return nil, fmt.Errorf("bad width")
	}

	height, err := strconv.Atoi(q.Get("height"))
	if err != nil || height <= 0 || height > 2000 {
		return nil, fmt.Errorf("bad height")
	}

	spawns, err := strconv.Atoi(q.Get("spawnscnt"))
	if err != nil || spawns < 0 || spawns > 50 {
		return nil, fmt.Errorf("bad spawnscnt")
	}

	bedrooms, err := strconv.Atoi(q.Get("bedroomcnt"))
	if err != nil || bedrooms < 0 || bedrooms > 50 {
		return nil, fmt.Errorf("bad bedroomcnt")
	}

	spawnR, err := strconv.Atoi(q.Get("spawnradius"))
	if err != nil || spawnR <= 0 {
		return nil, fmt.Errorf("bad spawnradius")
	}

	bedroomR, err := strconv.Atoi(q.Get("bedroomradius"))
	if err != nil || bedroomR <= 0 {
		return nil, fmt.Errorf("bad bedroomradius")
	}

	maxgap, err := strconv.Atoi(q.Get("maxgap"))
	if err != nil || maxgap <= 0 {
		return nil, fmt.Errorf("bad maxgap")
	}

	return &MapConfig{
		Width:         width,
		Height:        height,
		SpawnCount:    spawns,
		BedroomCount:  bedrooms,
		SpawnRadius:   spawnR,
		BedroomRadius: bedroomR,
		MaxGap:        maxgap,
	}, nil
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
	cfg, err := parseRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mapData := NewMapData(*cfg)
	if err := mapData.generate(); err != nil {
		resp := map[string]string{"error": err.Error()}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	img := mapData.render()

	w.Header().Set("Content-Type", "image/png")
	png.Encode(w, img)
}

func main() {
	http.HandleFunc("/map", mapHandler)

	log.Println("Server on :8080")
	log.Println("Try: http://localhost:8080/map?width=1000&height=1000&spawnscnt=7&bedroomcnt=4&spawnradius=20&bedroomradius=10&maxgap=5")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
