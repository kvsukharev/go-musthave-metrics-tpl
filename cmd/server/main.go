package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Конфигурация сервера
type ServerConfig struct {
	Address string
}

// Хранилище метрик в памяти с мьютексом для безопасности
var (
	gauges   = make(map[string]float64)
	counters = make(map[string]int64)
	mu       sync.RWMutex
)

func main() {
	config := parseServerFlags()

	// Создаем роутер chi
	r := chi.NewRouter()

	// Добавляем middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Регистрируем обработчики
	r.Post("/update/*", updateHandler)                        // Старый способ через path parsing
	r.Post("/update/{type}/{name}/{value}", updateHandlerChi) // Новый способ через chi params
	r.Get("/value/{type}/{name}", valueHandler)               // Новый endpoint для получения метрик
	r.Get("/", rootHandler)

	log.Printf("Starting metrics server on %s", config.Address)
	log.Fatal(http.ListenAndServe(config.Address, r))
}

// parseServerFlags парсит флаги командной строки для сервера
func parseServerFlags() *ServerConfig {
	config := &ServerConfig{}

	flag.StringVar(&config.Address, "a", "localhost:8080", "HTTP server endpoint address")

	// Парсим флаги
	flag.Parse()

	// Проверяем на неизвестные флаги
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unknown arguments: %v\n", flag.Args())
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	return config
}

// updateHandler - ваш оригинальный обработчик (сохранен для совместимости)
func updateHandler(w http.ResponseWriter, r *http.Request) {
	// Парсим URL: /update/{type}/{name}/{value}
	path := strings.TrimPrefix(r.URL.Path, "/update/")
	parts := strings.Split(path, "/")

	if len(parts) != 3 {
		http.Error(w, "Invalid URL format. Expected: /update/{type}/{name}/{value}",
			http.StatusBadRequest)
		return
	}

	metricType := parts[0]
	metricName := parts[1]
	metricValue := parts[2]

	updateMetric(w, metricType, metricName, metricValue)
}

// updateHandlerChi - новый обработчик с использованием chi URL параметров
func updateHandlerChi(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")
	metricValue := chi.URLParam(r, "value")

	updateMetric(w, metricType, metricName, metricValue)
}

// updateMetric - общая логика обновления метрик
func updateMetric(w http.ResponseWriter, metricType, metricName, metricValue string) {
	mu.Lock()
	defer mu.Unlock()

	switch metricType {
	case "gauge":
		value, err := strconv.ParseFloat(metricValue, 64)
		if err != nil {
			http.Error(w, "Invalid gauge value", http.StatusBadRequest)
			return
		}
		gauges[metricName] = value
		log.Printf("Updated gauge %s = %.6f", metricName, value)

	case "counter":
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			http.Error(w, "Invalid counter value", http.StatusBadRequest)
			return
		}
		counters[metricName] += value // Counters накапливаются
		log.Printf("Updated counter %s = %d (added %d)", metricName, counters[metricName], value)

	default:
		http.Error(w, "Unknown metric type. Use 'gauge' or 'counter'",
			http.StatusBadRequest)
		return
	}

	// Отправляем ответ
	responseText := "OK\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(responseText)))
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, responseText)
}

// valueHandler - НОВЫЙ обработчик для получения значения метрики
func valueHandler(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")

	mu.RLock()
	defer mu.RUnlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	switch metricType {
	case "gauge":
		value, exists := gauges[metricName]
		if !exists {
			http.Error(w, "Metric not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%g", value)

	case "counter":
		value, exists := counters[metricName]
		if !exists {
			http.Error(w, "Metric not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%d", value)

	default:
		http.Error(w, "Unknown metric type. Use 'gauge' or 'counter'", http.StatusBadRequest)
	}
}

// rootHandler - обновленный обработчик главной страницы с улучшенным HTML
func rootHandler(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()

	// Создаем копии для безопасной работы с шаблоном
	gaugesCopy := make(map[string]float64)
	countersCopy := make(map[string]int64)

	for k, v := range gauges {
		gaugesCopy[k] = v
	}
	for k, v := range counters {
		countersCopy[k] = v
	}

	tmpl := `<!DOCTYPE html>
<html>
<head>
    <title>Metrics Server</title>
    <style>
        body { 
            font-family: Arial, sans-serif; 
            margin: 40px; 
            background-color: #f5f5f5; 
        }
        .container {
            background-color: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        table { 
            border-collapse: collapse; 
            width: 100%; 
            margin-bottom: 20px; 
        }
        th, td { 
            border: 1px solid #ddd; 
            padding: 12px; 
            text-align: left; 
        }
        th { 
            background-color: #4CAF50; 
            color: white;
        }
        tr:nth-child(even) {
            background-color: #f2f2f2;
        }
        h1 { 
            color: #333; 
            text-align: center;
        }
        h2 { 
            color: #4CAF50; 
            border-bottom: 2px solid #4CAF50;
            padding-bottom: 10px;
        }
        .count {
            color: #666;
            font-size: 0.9em;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Metrics Server Dashboard</h1>
        
        <h2>Gauges <span class="count">({{len .Gauges}})</span></h2>
        <table>
            <tr><th>Name</th><th>Value</th></tr>
            {{range $name, $value := .Gauges}}
            <tr><td><strong>{{$name}}</strong></td><td>{{printf "%.6f" $value}}</td></tr>
            {{else}}
            <tr><td colspan="2" style="text-align: center; color: #666;">No gauges available</td></tr>
            {{end}}
        </table>
        
        <h2>Counters <span class="count">({{len .Counters}})</span></h2>
        <table>
            <tr><th>Name</th><th>Value</th></tr>
            {{range $name, $value := .Counters}}
            <tr><td><strong>{{$name}}</strong></td><td>{{$value}}</td></tr>
            {{else}}
            <tr><td colspan="2" style="text-align: center; color: #666;">No counters available</td></tr>
            {{end}}
        </table>
        
        <div style="margin-top: 30px; padding: 15px; background-color: #e7f3ff; border-left: 4px solid #2196F3;">
            <h3>API Endpoints:</h3>
            <ul>
                <li><code>POST /update/{type}/{name}/{value}</code> - Update metric</li>
                <li><code>GET /value/{type}/{name}</code> - Get metric value</li>
                <li><code>GET /</code> - This dashboard</li>
            </ul>
        </div>
    </div>
</body>
</html>`

	t, err := template.New("metrics").Parse(tmpl)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Printf("Template parse error: %v", err)
		return
	}

	data := struct {
		Gauges   map[string]float64
		Counters map[string]int64
	}{
		Gauges:   gaugesCopy,
		Counters: countersCopy,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := t.Execute(w, data); err != nil {
		log.Printf("Template execution error: %v", err)
	}
}
