package main

import (
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	RoadLength        = 5000.0 // метры (5 км)
	CarLength         = 4.5    // метры
	UpdateInterval    = 50     // миллисекунды
	ReactionTime      = 0.2    // секунды
	SafetyMultiplier  = 3.0    // коэффициент безопасной дистанции
	BrakeDeceleration = 6.67   // м/с² (примерно 15 миль/ч за секунду)
)

// Car представляет автомобиль
type Car struct {
	ID              int     `json:"id"`
	Position        float64 `json:"position"`        // метры от начала
	Speed           float64 `json:"speed"`           // м/с
	TargetSpeed     float64 `json:"targetSpeed"`     // желаемая скорость
	BrakeCount      int     `json:"brakeCount"`      // количество торможений
	Color           string  `json:"color"`           // цвет для визуализации
	State           string  `json:"state"`           // "normal", "braking", "accelerating"
	ReactionDelay   float64 `json:"reactionDelay"`   // время задержки реакции
	lastBrakeTime   float64 // для отслеживания задержки
}

// Simulation представляет симуляцию движения
type Simulation struct {
	Cars            []*Car          `json:"cars"`
	Time            float64         `json:"time"`
	CarsCompleted   int             `json:"carsCompleted"`
	TotalCarsMade   int             `json:"totalCarsMade"`
	Running         bool            `json:"running"`
	SpawnInterval   float64         `json:"spawnInterval"`   // секунды между машинами
	MinSpeed        float64         `json:"minSpeed"`        // м/с
	MaxSpeed        float64         `json:"maxSpeed"`        // м/с
	TimeScale       float64         `json:"timeScale"`       // множитель скорости времени (1.0 = нормально)
	MaxCars         int             `json:"maxCars"`         // максимальное количество машин для генерации
	mu              sync.RWMutex
	lastSpawn       float64
	nextCarID       int
}

// SimulationConfig конфигурация симуляции
type SimulationConfig struct {
	SpawnInterval float64 `json:"spawnInterval"` // секунды
	MinSpeed      float64 `json:"minSpeed"`      // км/ч
	MaxSpeed      float64 `json:"maxSpeed"`      // км/ч
	MaxCars       int     `json:"maxCars"`       // максимальное количество машин
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	simulation *Simulation
	clients    = make(map[*websocket.Conn]bool)
	clientsMu  sync.RWMutex
	broadcast  = make(chan []byte)
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// NewSimulation создает новую симуляцию
func NewSimulation() *Simulation {
	return &Simulation{
		Cars:          make([]*Car, 0),
		SpawnInterval: 2.0,
		MinSpeed:      kmhToMs(50),
		MaxSpeed:      kmhToMs(80),
		TimeScale:     1.0,
		MaxCars:       100,
		Running:       false,
	}
}

// kmhToMs конвертирует км/ч в м/с
func kmhToMs(kmh float64) float64 {
	return kmh / 3.6
}

// msToKmh конвертирует м/с в км/ч
func msToKmh(ms float64) float64 {
	return ms * 3.6
}

// randomSpeed возвращает случайную скорость в диапазоне
func (s *Simulation) randomSpeed() float64 {
	return s.MinSpeed + rand.Float64()*(s.MaxSpeed-s.MinSpeed)
}

// randomColor возвращает случайный цвет для автомобиля
func randomColor() string {
	colors := []string{"#FF6B6B", "#4ECDC4", "#45B7D1", "#FFA07A", "#98D8C8", "#F7DC6F", "#BB8FCE", "#85C1E2"}
	return colors[rand.Intn(len(colors))]
}

// SpawnCar создает новый автомобиль
func (s *Simulation) SpawnCar() {
	speed := s.randomSpeed()
	car := &Car{
		ID:            s.nextCarID,
		Position:      0,
		Speed:         speed,
		TargetSpeed:   speed,
		Color:         randomColor(),
		State:         "normal",
		ReactionDelay: 0,
	}
	s.Cars = append(s.Cars, car)
	s.nextCarID++
	s.TotalCarsMade++
}

// getSafeDistance вычисляет безопасную дистанцию
func getSafeDistance(speedDiff float64) float64 {
	// Преобразуем в км/ч для расчета (как в оригинале: 1 фут на милю/час разницы)
	speedDiffKmh := msToKmh(math.Abs(speedDiff))
	// 1 миля/час ≈ 1.6 км/ч, 1 фут ≈ 0.3 м
	safeDistance := (speedDiffKmh / 1.6) * 0.3 * SafetyMultiplier
	return math.Max(safeDistance, CarLength*2)
}

// Update обновляет состояние симуляции
func (s *Simulation) Update(dt float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.Running {
		return
	}

	// Применяем множитель скорости времени
	dt = dt * s.TimeScale
	s.Time += dt

	// Создаем новые автомобили
	if s.Time-s.lastSpawn >= s.SpawnInterval && s.TotalCarsMade < s.MaxCars {
		// Проверяем, что начало дороги свободно
		canSpawn := true
		for _, car := range s.Cars {
			if car.Position < 50 { // минимум 50м от начала
				canSpawn = false
				break
			}
		}
		if canSpawn {
			s.SpawnCar()
			s.lastSpawn = s.Time
		}
	}

	// Обновляем каждый автомобиль
	for i, car := range s.Cars {
		// Находим автомобиль впереди
		var carAhead *Car
		minDistance := math.MaxFloat64

		for j, other := range s.Cars {
			if i != j && other.Position > car.Position {
				distance := other.Position - car.Position
				if distance < minDistance {
					minDistance = distance
					carAhead = other
				}
			}
		}

		// Логика торможения и ускорения
		if carAhead != nil {
			distance := carAhead.Position - car.Position - CarLength
			speedDiff := car.Speed - carAhead.Speed
			safeDistance := getSafeDistance(speedDiff)

			if distance < safeDistance {
				// Нужно тормозить
				if car.State != "braking" || s.Time-car.lastBrakeTime > ReactionTime {
					car.State = "braking"
					car.Speed = math.Max(0, car.Speed-BrakeDeceleration*dt)
					if car.lastBrakeTime == 0 || s.Time-car.lastBrakeTime > 1.0 {
						car.BrakeCount++
						car.lastBrakeTime = s.Time
					}
				}
			} else if car.Speed < car.TargetSpeed {
				// Можно ускоряться
				car.State = "accelerating"
				acceleration := 2.0 // м/с²
				car.Speed = math.Min(car.TargetSpeed, car.Speed+acceleration*dt)
			} else {
				car.State = "normal"
			}
		} else {
			// Нет машины впереди - движемся к целевой скорости
			if car.Speed < car.TargetSpeed {
				car.State = "accelerating"
				acceleration := 2.0
				car.Speed = math.Min(car.TargetSpeed, car.Speed+acceleration*dt)
			} else {
				car.State = "normal"
			}
		}

		// Обновляем позицию
		car.Position += car.Speed * dt
	}

	// Удаляем автомобили, которые прошли дорогу
	newCars := make([]*Car, 0)
	for _, car := range s.Cars {
		if car.Position < RoadLength {
			newCars = append(newCars, car)
		} else {
			s.CarsCompleted++
		}
	}
	s.Cars = newCars

	// Автоматически останавливаем симуляцию, если достигнут лимит машин и все прошли дорогу
	if s.TotalCarsMade >= s.MaxCars && len(s.Cars) == 0 {
		s.Running = false
	}
}

// GetState возвращает текущее состояние симуляции
func (s *Simulation) GetState() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return struct {
		Cars          []*Car  `json:"cars"`
		Time          float64 `json:"time"`
		CarsCompleted int     `json:"carsCompleted"`
		TotalCarsMade int     `json:"totalCarsMade"`
		Running       bool    `json:"running"`
		RoadLength    float64 `json:"roadLength"`
		TimeScale     float64 `json:"timeScale"`
		MaxCars       int     `json:"maxCars"`
	}{
		Cars:          s.Cars,
		Time:          s.Time,
		CarsCompleted: s.CarsCompleted,
		TotalCarsMade: s.TotalCarsMade,
		Running:       s.Running,
		RoadLength:    RoadLength,
		TimeScale:     s.TimeScale,
		MaxCars:       s.MaxCars,
	}
}

// Start запускает симуляцию
func (s *Simulation) Start() {
	s.mu.Lock()
	s.Running = true
	s.mu.Unlock()
}

// Stop останавливает симуляцию
func (s *Simulation) Stop() {
	s.mu.Lock()
	s.Running = false
	s.mu.Unlock()
}

// Reset сбрасывает симуляцию
func (s *Simulation) Reset() {
	s.mu.Lock()
	s.Cars = make([]*Car, 0)
	s.Time = 0
	s.CarsCompleted = 0
	s.TotalCarsMade = 0
	s.Running = false
	s.lastSpawn = 0
	s.nextCarID = 0
	s.mu.Unlock()
}

// UpdateConfig обновляет конфигурацию
func (s *Simulation) UpdateConfig(config SimulationConfig) {
	s.mu.Lock()
	s.SpawnInterval = config.SpawnInterval
	s.MinSpeed = kmhToMs(config.MinSpeed)
	s.MaxSpeed = kmhToMs(config.MaxSpeed)
	if config.MaxCars > 0 {
		s.MaxCars = config.MaxCars
	}
	s.mu.Unlock()
}

// SetTimeScale устанавливает скорость времени
func (s *Simulation) SetTimeScale(scale float64) {
	s.mu.Lock()
	// Ограничиваем значения от 0.1x до 10x
	if scale < 0.1 {
		scale = 0.1
	}
	if scale > 10.0 {
		scale = 10.0
	}
	s.TimeScale = scale
	s.mu.Unlock()
}

// Handlers
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients, conn)
		clientsMu.Unlock()
	}()

	// Отправляем начальное состояние
	state := simulation.GetState()
	data, _ := json.Marshal(state)
	conn.WriteMessage(websocket.TextMessage, data)

	// Слушаем команды от клиента
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var cmd map[string]interface{}
		if err := json.Unmarshal(message, &cmd); err != nil {
			continue
		}

		switch cmd["action"] {
		case "start":
			simulation.Start()
		case "stop":
			simulation.Stop()
		case "reset":
			simulation.Reset()
		case "config":
			var config SimulationConfig
			configData, _ := json.Marshal(cmd["data"])
			json.Unmarshal(configData, &config)
			simulation.UpdateConfig(config)
		case "timescale":
			if scale, ok := cmd["value"].(float64); ok {
				simulation.SetTimeScale(scale)
			}
		}
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

// broadcastState отправляет состояние всем подключенным клиентам
func broadcastState() {
	for {
		state := simulation.GetState()
		data, err := json.Marshal(state)
		if err != nil {
			log.Println("JSON marshal error:", err)
			continue
		}

		clientsMu.RLock()
		for client := range clients {
			err := client.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				log.Println("WebSocket write error:", err)
				client.Close()
				clientsMu.Lock()
				delete(clients, client)
				clientsMu.Unlock()
			}
		}
		clientsMu.RUnlock()

		time.Sleep(time.Millisecond * UpdateInterval)
	}
}

// simulationLoop главный цикл симуляции
func simulationLoop() {
	ticker := time.NewTicker(time.Millisecond * UpdateInterval)
	defer ticker.Stop()

	for range ticker.C {
		simulation.Update(float64(UpdateInterval) / 1000.0)
	}
}

func main() {
	simulation = NewSimulation()

	// Запускаем цикл симуляции
	go simulationLoop()

	// Запускаем broadcast
	go broadcastState()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/ws", handleWebSocket)

	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
