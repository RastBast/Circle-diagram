package main

import (
    "fmt"
    "image"
    "image/color"
    "image/png"
    "net/http"
    "strconv"
    "time"
)

// Circle представляет круг в клеточной системе.
type Circle struct {
    X, Y   int  // координаты в клетках
    Radius int  // радиус в клетках
}

// MapGenerator хранит настройки карты.
type MapGenerator struct {
    Width, Height int // размеры в клетках
    SpawnCount    int
    BedroomCount  int
    SpawnRadius   int
    BedroomRadius int
    MaxGap        int // в клетках, но больше не используется
    CellSize      int // размер клетки в пикселях
}

// Проверка пересечения кругов по клеточному расстоянию.
func (c1 Circle) intersects(c2 Circle) bool {
    dx := c1.X - c2.X
    dy := c1.Y - c2.Y
    return dx*dx+dy*dy < (c1.Radius+c2.Radius)*(c1.Radius+c2.Radius)
}

// Генерация списка кругов.
func (mg *MapGenerator) generateCircles() ([]Circle, error) {
    randSrc := rand.NewSource(time.Now().UnixNano())
    rnd := rand.New(randSrc)

    var circles []Circle
    maxAttempts := 50000

    tryPlace := func(count, radius int) {
        attempts := 0
        for len(circles) < count && attempts < maxAttempts {
            attempts++
            x := rnd.Intn(mg.Width)
            y := rnd.Intn(mg.Height)
            newC := Circle{X: x, Y: y, Radius: radius}

            ok := true
            for _, ex := range circles {
                if newC.intersects(ex) {
                    ok = false
                    break
                }
            }
            if ok {
                circles = append(circles, newC)
            }
        }
    }

    // Сперва spawn, затем bedroom
    tryPlace(mg.SpawnCount, mg.SpawnRadius)
    tryPlace(mg.SpawnCount+mg.BedroomCount, mg.BedroomRadius)

    total := mg.SpawnCount + mg.BedroomCount
    if len(circles) < total {
        return nil, fmt.Errorf("не удалось разместить все круги без пересечений")
    }
    return circles, nil
}

// Рисование сетки и кругов.
func (mg *MapGenerator) generateMapImage() (*image.RGBA, error) {
    circles, err := mg.generateCircles()
    if err != nil {
        return nil, err
    }

    wPx := mg.Width * mg.CellSize
    hPx := mg.Height * mg.CellSize
    img := image.NewRGBA(image.Rect(0, 0, wPx, hPx))
    white := color.RGBA{255, 255, 255, 255}
    // заливаем фон
    for y := 0; y < hPx; y++ {
        for x := 0; x < wPx; x++ {
            img.Set(x, y, white)
        }
    }

    gridColor := color.RGBA{200, 200, 200, 255}
    // рисуем сетку
    for i := 0; i <= mg.Width; i++ {
        x := i * mg.CellSize
        for y := 0; y < hPx; y++ {
            img.Set(x, y, gridColor)
        }
    }
    for j := 0; j <= mg.Height; j++ {
        y := j * mg.CellSize
        for x := 0; x < wPx; x++ {
            img.Set(x, y, gridColor)
        }
    }

    circleColor := color.RGBA{0, 0, 255, 255} // единый цвет для всех кругов

    // закрашиваем клетки, попадающие в круг
    for _, c := range circles {
        for cy := c.Y - c.Radius; cy <= c.Y+c.Radius; cy++ {
            for cx := c.X - c.Radius; cx <= c.X+c.Radius; cx++ {
                if cx < 0 || cx >= mg.Width || cy < 0 || cy >= mg.Height {
                    continue
                }
                dx := cx - c.X
                dy := cy - c.Y
                if dx*dx+dy*dy <= c.Radius*c.Radius {
                    // закрасить всю клетку
                    x0 := cx * mg.CellSize
                    y0 := cy * mg.CellSize
                    for yy := 0; yy < mg.CellSize; yy++ {
                        for xx := 0; xx < mg.CellSize; xx++ {
                            img.Set(x0+xx, y0+yy, circleColor)
                        }
                    }
                }
            }
        }
    }

    return img, nil
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
    q := r.URL.Query()
    getInt := func(name string) (int, error) {
        v, err := strconv.Atoi(q.Get(name))
        return v, err
    }

    width, err := getInt("width")
    if err != nil || width <= 0 {
        http.Error(w, "Неверный width", http.StatusBadRequest)
        return
    }
    height, err := getInt("height")
    if err != nil || height <= 0 {
        http.Error(w, "Неверный height", http.StatusBadRequest)
        return
    }
    spCnt, err := getInt("spawnscnt")
    if err != nil || spCnt <= 0 {
        http.Error(w, "Неверный spawnscnt", http.StatusBadRequest)
        return
    }
    brCnt, err := getInt("bedroomcnt")
    if err != nil || brCnt <= 0 {
        http.Error(w, "Неверный bedroomcnt", http.StatusBadRequest)
        return
    }
    spRad, err := getInt("spawnradius")
    if err != nil || spRad <= 0 {
        http.Error(w, "Неверный spawnradius", http.StatusBadRequest)
        return
    }
    brRad, err := getInt("bedroomradius")
    if err != nil || brRad <= 0 {
        http.Error(w, "Неверный bedroomradius", http.StatusBadRequest)
        return
    }

    mg := &MapGenerator{
        Width:         width,
        Height:        height,
        SpawnCount:    spCnt,
        BedroomCount:  brCnt,
        SpawnRadius:   spRad,
        BedroomRadius: brRad,
        CellSize:      50, // например, 50px на клетку
    }

    img, err := mg.generateMapImage()
    if err != nil {
        http.Error(w, fmt.Sprintf("Ошибка генерации: %v", err), http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "image/png")
    png.Encode(w, img)
}

func main() {
    http.HandleFunc("/generate-map", mapHandler)
    fmt.Println("Сервер запущен на :8080")
    http.ListenAndServe(":8080", nil)
}
