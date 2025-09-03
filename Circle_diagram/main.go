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
    Width     int `json:"width"`
    Height    int `json:"height"`
    Spawns    int `json:"spawn_count"`
    Bedrooms  int `json:"bedroom_count"`
    SpawnR    int `json:"spawn_radius"`
    BedroomR  int `json:"bedroom_radius"`
    MaxGap    int `json:"max_gap"`
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

// Запросы новых эндпоинтов
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
    createTableSQL := `CREATE TABLE IF NOT EXISTS maps (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        config TEXT NOT NULL,
        circles TEXT NOT NULL,
        speeds TEXT DEFAULT '',
        epoch INTEGER DEFAULT 0,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );`
    _, err = db.Exec(createTableSQL)
    return err
}

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
        centerSpawn := Circle{
            X:      g.config.Width / 2,
            Y:      g.config.Height / 2,
            Radius: g.config.SpawnR,
        }
        if g.canPlaceCircle(centerSpawn) {
            g.spawns = append(g.spawns, centerSpawn)
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

func getCellType(x, y int, circles []Circle) int {
    for _, circle := range circles {
        dx := x - circle.X
        dy := y - circle.Y
        if dx == 0 && dy == 0 {
            return 2
        }
        if dx*dx+dy*dy <= circle.Radius*circle.Radius {
            return 1
        }
    }
    return 0
}

// --- Новый функционал третей части ---

// Получение соседних клеток
func getNeighbors(x, y int, cfg Config) []struct{ X, Y int } {
    neighbors := []struct{ X, Y int }{}
    directions := []struct{ dx, dy int }{
        {-1, -1}, {-1, 0}, {-1, 1},
        {0, -1}, {0, 1},
        {1, -1}, {1, 0}, {1, 1},
    }
    for _, dir := range directions {
        nx, ny := x+dir.dx, y+dir.dy
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
    currentState := make(map[string][]int)
    for _, cell := range cells {
        key := fmt.Sprintf("%d,%d", cell.X, cell.Y)
        currentState[key] = append([]int{}, cell.Vals...)
    }
    for _, cell := range cells {
        remainingNumbers := []int{}
        for _, num := range cell.Vals {
            speedIndex := num
            if speedIndex >= len(speeds) {
                speedIndex = 0
            }
            speed := speeds[speedIndex]
            if rand.Float64()*100 < speed {
                neighbors := getNeighbors(cell.X, cell.Y, cfg)
                moved := false
                for _, neighbor := range neighbors {
                    neighborType := getCellType(neighbor.X, neighbor.Y, circles)
                    neighborKey := fmt.Sprintf("%d,%d", neighbor.X, neighbor.Y)
                    currentCount := len(currentState[neighborKey])
                    canMove := false
                    switch neighborType {
                    case 0:
                        canMove = currentCount < 2
                    case 1:
                        canMove = currentCount < 1
                    case 2:
                        canMove = false
                    }
                    if canMove {
                        currentState[neighborKey] = append(currentState[neighborKey], num)
                        moved = true
                        break
                    }
                }
                if !moved {
                    remainingNumbers = append(remainingNumbers, num)
                }
            } else {
                remainingNumbers = append(remainingNumbers, num)
            }
        }
        key := fmt.Sprintf("%d,%d", cell.X, cell.Y)
        currentState[key] = remainingNumbers
    }
    newCells := make([]Cell, 0)
    for y := 0; y < cfg.Height; y++ {
        for x := 0; x < cfg.Width; x++ {
            key := fmt.Sprintf("%d,%d", x, y)
            if vals, exists := currentState[key]; exists && len(vals) > 0 {
                newCells = append(newCells, Cell{
                    X:    x,
                    Y:    y,
                    Vals: vals,
                })
            }
        }
    }
    return newCells
}

// Эндпоинт POST /api/speeds
func setSpeedsHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    var request SetSpeedsRequest
    err := json.NewDecoder(r.Body).Decode(&request)
    if err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    var exists bool
    err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM maps WHERE id = ?)", request.MapID).Scan(&exists)
    if err != nil || !exists {
        http.Error(w, "Карта не найдена", http.StatusNotFound)
        return
    }
    speedsJSON, _ := json.Marshal(request.Speeds)
    _, err = db.Exec("UPDATE maps SET speeds = ? WHERE id = ?", string(speedsJSON), request.MapID)
    if err != nil {
        http.Error(w, "Ошибка сохранения скоростей", http.StatusInternalServerError)
        return
    }
    response := struct {
        MapID   int       `json:"map_id"`
        Speeds  []float64 `json:"speeds"`
        Success bool      `json:"success"`
    }{request.MapID, request.Speeds, true}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// Эндпоинт POST /api/newEpoch
func newEpochHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    var request NewEpochRequest
    err := json.NewDecoder(r.Body).Decode(&request)
    if err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    var configJSON, circlesJSON, speedsJSON string
    var currentEpoch int
    err = db.QueryRow("SELECT config, circles, speeds, epoch FROM maps WHERE id = ?", request.MapID).
        Scan(&configJSON, &circlesJSON, &speedsJSON, &currentEpoch)
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
    var speeds []float64
    json.Unmarshal([]byte(configJSON), &config)
    json.Unmarshal([]byte(circlesJSON), &circles)
    if speedsJSON != "" {
        json.Unmarshal([]byte(speedsJSON), &speeds)
    }
    currentCells := generateDistribution(config, circles, []float64{90.0, 10.0})
    if len(speeds) > 0 {
        currentCells = moveNumbers(config, circles, currentCells, speeds)
    }
    newEpoch := currentEpoch + 1
    _, err = db.Exec("UPDATE maps SET epoch = ? WHERE id = ?", newEpoch, request.MapID)
    if err != nil {
        http.Error(w, "Ошибка обновления эпохи", http.StatusInternalServerError)
        return
    }
    response := struct {
        MapID int    `json:"map_id"`
        Epoch int    `json:"epoch"`
        Cells []Cell `json:"cells"`
    }{request.MapID, newEpoch, currentCells}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// --- Остальные функции (без изменений, для полноценной работы) ---

func createProbabilitySelector(probabilities []float64) []int {
    selector := make([]int, 0)
    for index, probability := range probabilities {
        count := int(probability * 50)
        for j := 0; j < count; j++ {
            selector = append(selector, index)
        }
    }
    return selector
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
            case 2:
                values = []int{0}
            case 1:
                values = []int{selector[rand.Intn(len(selector))]}
            case 0:
                count := 1 + rand.Intn(2)
                values = make([]int, count)
                for i := 0; i < count; i++ {
                    values[i] = selector[rand.Intn(len(selector))]
                }
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

// --- Endpoints для существующей функциональности ---
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
        MapID        int       `json:"map_id"`
        Probabilities []float64 `json:"probabilities"`
    }
    err := json.NewDecoder(r.Body).Decode(&request)
    if err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
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
    cells := generateDistribution(config, circles, request.Probabilities)
    response := struct {
        MapID int    `json:"map_id"`
        Cells []Cell `json:"cells"`
    }{request.MapID, cells}
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
    switch {
    case r.URL.Path == "/api/maps" && r.Method == "POST":
        createMapHandler(w, r)
    case r.URL.Path == "/api/distribute" && r.Method == "POST":
        distributeHandler(w, r)
    case r.URL.Path == "/api/speeds" && r.Method == "POST":
        setSpeedsHandler(w, r)
    case r.URL.Path == "/api/newEpoch" && r.Method == "POST":
        newEpochHandler(w, r)
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
    log.Println("Сервер запущен на порту :8080")
    log.Println("Доступные endpoints:")
    log.Println(" POST /api/maps - создание карты")
    log.Println(" POST /api/distribute - распределение индексов")
    log.Println(" POST /api/speeds - установка скоростей")
    log.Println(" POST /api/newEpoch - переключение эпохи")
    log.Fatal(http.ListenAndServe(":8080", nil))
}