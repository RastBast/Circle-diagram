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

type Cell struct {
	X    int   `json:"x"`
	Y    int   `json:"y"`
	Vals []int `json:"indices"`
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
		cnt := int(prob * 50) // достаточная точность
		for j := 0; j < cnt; j++ {
			sel = append(sel, i)
		}
	}

	return sel
}

// Четкие bitmap шрифты для цифр 15×20 пикселей
func getDigitBitmap(digit rune) [][]bool {
	switch digit {
	case '0':
		return [][]bool{
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '1':
		return [][]bool{
			{false, false, false, false, false, false, true, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, true, true, true, true, false, false, false, false, false, false},
			{false, false, false, false, true, true, true, true, true, false, false, false, false, false, false},
			{false, false, false, true, true, true, false, true, true, false, false, false, false, false, false},
			{false, false, true, true, true, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, false, false, false, false, false, false},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '2':
		return [][]bool{
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, true, true, true, false},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, true, true, true, false, false, false},
			{false, false, false, false, false, false, false, false, true, true, true, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, true, false, false, false, false, false},
			{false, false, false, false, false, false, true, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, true, true, true, false, false, false, false, false, false, false},
			{false, false, false, false, true, true, true, false, false, false, false, false, false, false, false},
			{true, true, false, true, true, true, false, false, false, false, false, false, false, false, false},
			{true, true, true, true, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '3':
		return [][]bool{
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, true, false},
			{false, false, false, false, false, false, true, true, true, true, true, true, false, false, false},
			{false, false, false, false, false, false, true, true, true, true, true, true, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, true, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '4':
		return [][]bool{
			{false, false, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, true, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, true, true, true, true, true, false, false},
			{false, false, false, false, false, false, false, true, true, false, true, true, true, false, false},
			{false, false, false, false, false, false, true, true, true, false, true, true, true, false, false},
			{false, false, false, false, false, true, true, true, false, false, true, true, true, false, false},
			{false, false, false, false, true, true, true, false, false, false, true, true, true, false, false},
			{false, false, false, true, true, true, false, false, false, false, true, true, true, false, false},
			{false, false, true, true, true, false, false, false, false, false, true, true, true, false, false},
			{false, true, true, true, false, false, false, false, false, false, true, true, true, false, false},
			{true, true, true, false, false, false, false, false, false, false, true, true, true, false, false},
			{true, true, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '5':
		return [][]bool{
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '6':
		return [][]bool{
			{false, false, false, false, true, true, true, true, true, true, true, true, false, false, false},
			{false, false, false, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, false, true, true, true, false, false, false, false, false, false, false, true, true, false},
			{false, true, true, true, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, true, true, true, true, true, true, true, true, true, true, false, false},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '7':
		return [][]bool{
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, true, true, true, false},
			{false, false, false, false, false, false, false, false, false, false, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, true, true, true, false, false, false},
			{false, false, false, false, false, false, false, false, true, true, true, false, false, false, false},
			{false, false, false, false, false, false, false, true, true, true, false, false, false, false, false},
			{false, false, false, false, false, false, true, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, true, true, true, false, false, false, false, false, false, false},
			{false, false, false, false, true, true, true, false, false, false, false, false, false, false, false},
			{false, false, false, true, true, true, false, false, false, false, false, false, false, false, false},
			{false, false, true, true, true, false, false, false, false, false, false, false, false, false, false},
			{false, true, true, true, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '8':
		return [][]bool{
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case '9':
		return [][]bool{
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, false},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, true, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, true, true, true, true, true, true, true, true, true, true, true, true},
			{false, false, true, true, true, true, true, true, true, true, true, true, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, true, true},
			{true, true, false, false, false, false, false, false, false, false, false, false, true, true, true},
			{false, true, true, false, false, false, false, false, false, false, false, true, true, true, false},
			{false, false, true, true, true, true, true, true, true, true, true, true, true, false, false},
			{false, false, false, true, true, true, true, true, true, true, true, true, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	case ',':
		return [][]bool{
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, true, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, true, true, true, false, false, false, false, false, false},
			{false, false, false, false, false, false, true, true, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, true, false, false, false, false, false, false, false},
			{false, false, false, false, false, true, true, true, false, false, false, false, false, false, false},
			{false, false, false, false, false, false, false, false, false, false, false, false, false, false, false},
		}
	default:
		// Пустой bitmap для неизвестных символов
		return make([][]bool, 20)
	}
}

// Отрисовка четкой цифры 15×20 пикселей
func drawLargeDigit(img *image.RGBA, x, y int, digit rune, col color.Color) {
	bitmap := getDigitBitmap(digit)
	for py := 0; py < len(bitmap); py++ {
		for px := 0; px < len(bitmap[py]); px++ {
			if bitmap[py][px] {
				img.Set(x+px, y+py, col)
			}
		}
	}
}

// Рендеринг с крупными клетками 100×100 и четкими цифрами
func renderLargeCells(cfg Config, circles []Circle, probs []float64) *image.RGBA {
	cellSize := 100 // ФИКСИРОВАННЫЙ размер 100×100

	// Ограничение размера карты для экономии памяти
	maxCells := 500 // максимум 500×500 клеток = 250МБ изображение
	if cfg.Width > maxCells {
		cfg.Width = maxCells
	}
	if cfg.Height > maxCells {
		cfg.Height = maxCells
	}

	w := cfg.Width * cellSize
	h := cfg.Height * cellSize
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// Цвета
	white := color.RGBA{255, 255, 255, 255}
	gray := color.RGBA{128, 128, 128, 255}
	blue := color.RGBA{100, 150, 255, 255}
	green := color.RGBA{100, 255, 100, 255}
	black := color.RGBA{0, 0, 0, 255}

	sel := makeSelector(probs)
	if len(sel) == 0 {
		sel = []int{0}
	}

	// Генерация по клеткам
	for mapY := 0; mapY < cfg.Height; mapY++ {
		for mapX := 0; mapX < cfg.Width; mapX++ {
			ct := cellType(mapX, mapY, circles)

			startX := mapX * cellSize
			startY := mapY * cellSize

			// Определяем цвет фона и индексы
			var bgColor color.Color
			var indices []int

			switch ct {
			case 2: // зеленая (центр)
				bgColor = green
				indices = []int{0}
			case 1: // синяя (внутри круга)
				bgColor = blue
				indices = []int{sel[rand.Intn(len(sel))]}
			case 0: // белая (пустая) - ТОЛЬКО ОДИН ИНДЕКС
				bgColor = white
				indices = []int{sel[rand.Intn(len(sel))]}
			}

			// Заливаем клетку
			for py := 0; py < cellSize; py++ {
				for px := 0; px < cellSize; px++ {
					img.Set(startX+px, startY+py, bgColor)
				}
			}

			// Рамка клетки
			for py := 0; py < cellSize; py++ {
				img.Set(startX+cellSize-1, startY+py, gray) // правая
				img.Set(startX, startY+py, gray)            // левая
			}
			for px := 0; px < cellSize; px++ {
				img.Set(startX+px, startY+cellSize-1, gray) // нижняя
				img.Set(startX+px, startY, gray)            // верхняя
			}

			// ЧЕТКИЕ цифры в центре клетки
			if len(indices) > 0 && indices[0] >= 0 && indices[0] <= 9 {
				centerX := startX + (cellSize-15)/2
				centerY := startY + (cellSize-20)/2
				drawLargeDigit(img, centerX, centerY, rune('0'+indices[0]), black)
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

// Legacy handler с крупными клетками и четкими цифрами
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
	probs := []float64{90.0, 10.0} // по умолчанию
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

	// Рендеринг с крупными клетками
	img := renderLargeCells(cfg, circles, probs)

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
			case 0: // white - ТОЛЬКО ОДИН ИНДЕКС
				vals = []int{sel[rand.Intn(len(sel))]}
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

func distributeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "bad method", 405)
		return
	}

	var req struct {
		MapID int       `json:"map_id"`
		Probs []float64 `json:"probabilities"`
	}
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

	resp := struct {
		MapID int    `json:"map_id"`
		Cells []Cell `json:"cells"`
	}{req.MapID, cells}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/maps" && r.Method == "POST":
		createHandler(w, r)
	case r.URL.Path == "/api/distribute" && r.Method == "POST":
		distributeHandler(w, r)
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
	log.Println("✓ Large 100×100px cells with crisp digits")
	log.Println("✓ Single number per white cell")
	log.Println("✓ High quality 15×20px bitmap fonts")
	log.Println("✓ Parameter: ?probs=70,25,5")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
