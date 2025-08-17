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

// Circle представляет круг на карте
type Circle struct {
	X, Y   int
	Radius int
	Type   string // "spawn" или "bedroom"
}

// MapGenerator генерирует карту с кругами
type MapGenerator struct {
	Width         int
	Height        int
	SpawnCnt      int
	BedroomCnt    int
	SpawnRadius   int
	BedroomRadius int
	MaxGap        int
	Circles       []Circle
}

// Проверяет, пересекаются ли два круга (или касаются)
func (mg *MapGenerator) circlesIntersect(c1, c2 Circle) bool {
	dx := c1.X - c2.X
	dy := c1.Y - c2.Y
	distance := math.Sqrt(float64(dx*dx + dy*dy))
	// Круги пересекаются, если расстояние меньше суммы радиусов
	// Касание допустимо (distance == sum), пересечение нет (distance < sum)
	return distance < float64(c1.Radius+c2.Radius)
}

// Проверяет, находится ли круг в границах карты
func (mg *MapGenerator) isCircleInBounds(c Circle) bool {
	return c.X-c.Radius >= 0 && c.X+c.Radius < mg.Width &&
		c.Y-c.Radius >= 0 && c.Y+c.Radius < mg.Height
}

// Находит расстояние до ближайшего круга
func (mg *MapGenerator) findNearestDistance(c Circle) float64 {
	minDistance := math.Inf(1)
	for _, other := range mg.Circles {
		// Пропускаем сам круг
		if other.X == c.X && other.Y == c.Y && other.Radius == c.Radius {
			continue
		}
		dx := c.X - other.X
		dy := c.Y - other.Y
		distance := math.Sqrt(float64(dx*dx + dy*dy))
		if distance < minDistance {
			minDistance = distance
		}
	}
	return minDistance
}

// Генерирует карту с кругами
func (mg *MapGenerator) Generate() error {
	rand.Seed(time.Now().UnixNano())
	mg.Circles = make([]Circle, 0)

	maxAttempts := 10000
	totalCircles := mg.SpawnCnt + mg.BedroomCnt

	// Проверяем, что задача вообще выполнима
	if totalCircles == 0 {
		return nil // Нет кругов для размещения
	}

	// Размещаем spawn круги
	for i := 0; i < mg.SpawnCnt; i++ {
		placed := false
		for attempt := 0; attempt < maxAttempts; attempt++ {
			circle := Circle{
				X:      rand.Intn(mg.Width),
				Y:      rand.Intn(mg.Height),
				Radius: mg.SpawnRadius,
				Type:   "spawn",
			}

			if !mg.isCircleInBounds(circle) {
				continue
			}

			// Проверяем пересечения с существующими кругами
			valid := true
			for _, existing := range mg.Circles {
				if mg.circlesIntersect(circle, existing) {
					valid = false
					break
				}
			}

			if valid {
				mg.Circles = append(mg.Circles, circle)
				placed = true
				break
			}
		}
		if !placed {
			return fmt.Errorf("не удалось разместить spawn круг %d из %d", i+1, mg.SpawnCnt)
		}
	}

	// Размещаем bedroom круги
	for i := 0; i < mg.BedroomCnt; i++ {
		placed := false
		for attempt := 0; attempt < maxAttempts; attempt++ {
			circle := Circle{
				X:      rand.Intn(mg.Width),
				Y:      rand.Intn(mg.Height),
				Radius: mg.BedroomRadius,
				Type:   "bedroom",
			}

			if !mg.isCircleInBounds(circle) {
				continue
			}

			// Проверяем пересечения с существующими кругами
			valid := true
			for _, existing := range mg.Circles {
				if mg.circlesIntersect(circle, existing) {
					valid = false
					break
				}
			}

			if valid {
				mg.Circles = append(mg.Circles, circle)
				placed = true
				break
			}
		}
		if !placed {
			return fmt.Errorf("не удалось разместить bedroom круг %d из %d", i+1, mg.BedroomCnt)
		}
	}

	// Проверяем условие maxgap для всех кругов
	if len(mg.Circles) > 1 { // Проверяем только если есть больше одного круга
		for i, circle := range mg.Circles {
			// Временно убираем текущий круг из списка для поиска ближайшего
			tempCircles := make([]Circle, 0, len(mg.Circles)-1)
			for j, other := range mg.Circles {
				if i != j {
					tempCircles = append(tempCircles, other)
				}
			}

			// Находим ближайший круг
			minDistance := math.Inf(1)
			for _, other := range tempCircles {
				dx := circle.X - other.X
				dy := circle.Y - other.Y
				distance := math.Sqrt(float64(dx*dx + dy*dy))
				if distance < minDistance {
					minDistance = distance
				}
			}

			if minDistance > float64(mg.MaxGap) {
				return fmt.Errorf("круг %s на позиции (%d, %d) не имеет соседей в радиусе %d клеток (ближайший на расстоянии %.1f)",
					circle.Type, circle.X, circle.Y, mg.MaxGap, minDistance)
			}
		}
	}

	return nil
}

// Создает изображение карты
func (mg *MapGenerator) CreateImage() *image.RGBA {
	cellSize := 100
	imgWidth := mg.Width * cellSize
	imgHeight := mg.Height * cellSize

	img := image.NewRGBA(image.Rect(0, 0, imgWidth, imgHeight))

	// Заливаем белым цветом
	white := color.RGBA{255, 255, 255, 255}
	for y := 0; y < imgHeight; y++ {
		for x := 0; x < imgWidth; x++ {
			img.Set(x, y, white)
		}
	}

	// Рисуем координатную сетку
	gray := color.RGBA{200, 200, 200, 255}
	for i := 0; i <= mg.Width; i++ {
		x := i * cellSize
		for y := 0; y < imgHeight; y++ {
			if x < imgWidth {
				img.Set(x, y, gray)
			}
		}
	}
	for i := 0; i <= mg.Height; i++ {
		y := i * cellSize
		for x := 0; x < imgWidth; x++ {
			if y < imgHeight {
				img.Set(x, y, gray)
			}
		}
	}

	// Создаем карту клеток для отслеживания того, что уже закрашено
	cellColors := make([][]color.RGBA, mg.Height)
	for i := range cellColors {
		cellColors[i] = make([]color.RGBA, mg.Width)
		for j := range cellColors[i] {
			cellColors[i][j] = white // По умолчанию белый
		}
	}

	// Рисуем круги (сначала все круги синим)
	blue := color.RGBA{0, 0, 255, 255} // Синий для кругов

	for _, circle := range mg.Circles {
		// Рисуем круг закрашиванием клеток
		for dy := -circle.Radius; dy <= circle.Radius; dy++ {
			for dx := -circle.Radius; dx <= circle.Radius; dx++ {
				// Проверяем, находится ли точка в круге
				if dx*dx+dy*dy <= circle.Radius*circle.Radius {
					cellX := circle.X + dx
					cellY := circle.Y + dy

					// Проверяем границы карты
					if cellX >= 0 && cellX < mg.Width && cellY >= 0 && cellY < mg.Height {
						cellColors[cellY][cellX] = blue
					}
				}
			}
		}
	}

	// Теперь закрашиваем центры кругов зеленым
	green := color.RGBA{0, 255, 0, 255} // Зеленый для центров
	for _, circle := range mg.Circles {
		if circle.X >= 0 && circle.X < mg.Width && circle.Y >= 0 && circle.Y < mg.Height {
			cellColors[circle.Y][circle.X] = green
		}
	}

	// Применяем цвета клеток к изображению
	for cellY := 0; cellY < mg.Height; cellY++ {
		for cellX := 0; cellX < mg.Width; cellX++ {
			cellColor := cellColors[cellY][cellX]
			// Закрашиваем всю клетку выбранным цветом
			for py := 0; py < cellSize; py++ {
				for px := 0; px < cellSize; px++ {
					imgX := cellX*cellSize + px
					imgY := cellY*cellSize + py
					if imgX < imgWidth && imgY < imgHeight {
						// Не перезаписываем линии сетки
						currentColor := img.RGBAAt(imgX, imgY)
						if currentColor != gray {
							img.Set(imgX, imgY, cellColor)
						}
					}
				}
			}
		}
	}

	// Перерисовываем сетку поверх всего
	for i := 0; i <= mg.Width; i++ {
		x := i * cellSize
		for y := 0; y < imgHeight; y++ {
			if x < imgWidth {
				img.Set(x, y, gray)
			}
		}
	}
	for i := 0; i <= mg.Height; i++ {
		y := i * cellSize
		for x := 0; x < imgWidth; x++ {
			if y < imgHeight {
				img.Set(x, y, gray)
			}
		}
	}

	return img
}

// HTTP обработчик
func mapHandler(w http.ResponseWriter, r *http.Request) {
	// Парсим параметры
	query := r.URL.Query()

	width, err := strconv.ParseInt(query.Get("width"), 10, 64)
	if err != nil || width <= 0 {
		http.Error(w, "Неверный параметр width", http.StatusBadRequest)
		return
	}

	height, err := strconv.ParseInt(query.Get("height"), 10, 64)
	if err != nil || height <= 0 {
		http.Error(w, "Неверный параметр height", http.StatusBadRequest)
		return
	}

	spawnCnt, err := strconv.ParseInt(query.Get("spawnscnt"), 10, 64)
	if err != nil || spawnCnt < 0 {
		http.Error(w, "Неверный параметр spawnscnt", http.StatusBadRequest)
		return
	}

	bedroomCnt, err := strconv.ParseInt(query.Get("bedroomcnt"), 10, 64)
	if err != nil || bedroomCnt < 0 {
		http.Error(w, "Неверный параметр bedroomcnt", http.StatusBadRequest)
		return
	}

	spawnRadius, err := strconv.ParseInt(query.Get("spawnradius"), 10, 64)
	if err != nil || spawnRadius <= 0 {
		http.Error(w, "Неверный параметр spawnradius", http.StatusBadRequest)
		return
	}

	bedroomRadius, err := strconv.ParseInt(query.Get("bedroomradius"), 10, 64)
	if err != nil || bedroomRadius <= 0 {
		http.Error(w, "Неверный параметр bedroomradius", http.StatusBadRequest)
		return
	}

	maxGap, err := strconv.ParseInt(query.Get("maxgap"), 10, 64)
	if err != nil || maxGap <= 0 {
		http.Error(w, "Неверный параметр maxgap", http.StatusBadRequest)
		return
	}

	// Создаем генератор карты
	generator := &MapGenerator{
		Width:         int(width),
		Height:        int(height),
		SpawnCnt:      int(spawnCnt),
		BedroomCnt:    int(bedroomCnt),
		SpawnRadius:   int(spawnRadius),
		BedroomRadius: int(bedroomRadius),
		MaxGap:        int(maxGap),
	}

	// Генерируем карту
	err = generator.Generate()
	if err != nil {
		errorResponse := map[string]string{"error": err.Error()}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errorResponse)
		return
	}

	// Создаем изображение
	img := generator.CreateImage()

	// Отправляем PNG изображение
	w.Header().Set("Content-Type", "image/png")
	err = png.Encode(w, img)
	if err != nil {
		http.Error(w, "Ошибка создания изображения", http.StatusInternalServerError)
		return
	}
}

func main() {
	http.HandleFunc("/map", mapHandler)

	fmt.Println("Сервер запущен на порту 8080")
	fmt.Println("Пример запроса: http://localhost:8080/map?width=10&height=10&spawnscnt=2&bedroomcnt=3&spawnradius=2&bedroomradius=1&maxgap=5")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
