package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/kvsukharev/go-musthave-metrics-tpl/internal/middleware_proj"
)

type ServerConfig struct {
	Address       string `env:"ADDRESS"`
	StoreInterval time.Duration
	FileStorage   string
	Restore       bool
}

const (
	defaultStoreInterval = 300 * time.Second
	defaultFileStorage   = "metrics.json"
	defaultRestore       = false
)

const (
	defaultPollInterval   = 2 * time.Second
	defaultReportInterval = 10 * time.Second
	defaultServerAddress  = "localhost:8080"
	configPath            = "internal/config/agent.yaml"
)

type Metrics struct {
	ID    string   `json:"id"`              // имя метрики
	MType string   `json:"type"`            // параметр, принимающий значение gauge или counter
	Delta *int64   `json:"delta,omitempty"` // значение метрики в случае передачи counter
	Value *float64 `json:"value,omitempty"` // значение метрики в случае передачи gauge
}

type MetricsStorage struct {
	gauges   map[string]float64
	counters map[string]int64
	mu       sync.RWMutex
}

func NewMetricsStorage() *MetricsStorage {
	return &MetricsStorage{
		gauges:   make(map[string]float64),
		counters: make(map[string]int64),
	}
}

type Server struct {
	storage *MetricsStorage
	config  *ServerConfig
}

func NewServer(storage *MetricsStorage, config *ServerConfig) *Server {
	return &Server{
		storage: storage,
		config:  config,
	}
}

func loadServerConfig() (*ServerConfig, error) {
	cfg := &ServerConfig{
		Address: defaultServerAddress,
	}

	// env.Parse подтянет ADDRESS если есть
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	var flagAddress string
	flag.StringVar(&flagAddress, "a", "", "HTTP server address")
	flag.Parse()

	if os.Getenv("ADDRESS") == "" && flagAddress != "" {
		cfg.Address = flagAddress
	}

	return cfg, nil
}

func (s *Server) updateMetricJSONHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "cannot read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var m Metrics
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if m.ID == "" || (m.MType != "gauge" && m.MType != "counter") {
		http.Error(w, "invalid metric id or type", http.StatusBadRequest)
		return
	}

	s.storage.mu.Lock()
	defer s.storage.mu.Unlock()

	switch m.MType {
	case "gauge":
		if m.Value == nil {
			http.Error(w, "missing value for gauge", http.StatusBadRequest)
			return
		}
		s.storage.gauges[m.ID] = *m.Value
	case "counter":
		if m.Delta == nil {
			http.Error(w, "missing delta for counter", http.StatusBadRequest)
			return
		}
		s.storage.counters[m.ID] += *m.Delta
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) valueMetricJSONHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "cannot read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var req Metrics
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if req.ID == "" || (req.MType != "gauge" && req.MType != "counter") {
		http.Error(w, "invalid metric id or type", http.StatusBadRequest)
		return
	}

	s.storage.mu.RLock()
	defer s.storage.mu.RUnlock()

	resp := Metrics{ID: req.ID, MType: req.MType}

	switch req.MType {
	case "gauge":
		val, ok := s.storage.gauges[req.ID]
		if !ok {
			http.Error(w, "metric not found", http.StatusNotFound)
			return
		}
		resp.Value = &val
	case "counter":
		val, ok := s.storage.counters[req.ID]
		if !ok {
			http.Error(w, "metric not found", http.StatusNotFound)
			return
		}
		resp.Delta = &val
	}

	jsonResp, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonResp)
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware_proj.GzipMiddleware)

	r.Post("/update", s.updateMetricJSONHandler)
	r.Post("/value", s.valueMetricJSONHandler)
	r.Post("/update/*", s.updateHandler)
	r.Post("/update/{type}/{name}/{value}", s.updateHandlerChi)
	r.Get("/value/{type}/{name}", s.valueHandler)
	r.Get("/", s.rootHandler)

	r.Post("/update", s.updateMetricJSONHandler)
	r.Post("/value", s.valueMetricJSONHandler)

	return r
}

func (s *Server) updateHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/update/")
	parts := strings.Split(path, "/")

	if len(parts) != 3 {
		http.Error(w, "Invalid URL format. Expected: /update/{type}/{name}/{value}",
			http.StatusBadRequest)
		return
	}

	s.updateMetric(w, parts[0], parts[1], parts[2])
}

func (s *Server) updateHandlerChi(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")
	metricValue := chi.URLParam(r, "value")

	s.updateMetric(w, metricType, metricName, metricValue)
}

func (s *Server) updateMetric(w http.ResponseWriter, metricType, metricName, metricValue string) {
	s.storage.mu.Lock()
	defer s.storage.mu.Unlock()

	switch metricType {
	case "gauge":
		value, err := strconv.ParseFloat(metricValue, 64)
		if err != nil {
			http.Error(w, "Invalid gauge value", http.StatusBadRequest)
			return
		}
		s.storage.gauges[metricName] = value
		log.Printf("Updated gauge %s = %.6f", metricName, value)

	case "counter":
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			http.Error(w, "Invalid counter value", http.StatusBadRequest)
			return
		}
		s.storage.counters[metricName] += value
		log.Printf("Updated counter %s = %d (added %d)", metricName, s.storage.counters[metricName], value)

	default:
		http.Error(w, "Unknown metric type. Use 'gauge' or 'counter'",
			http.StatusBadRequest)
		return
	}

	responseText := "OK\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(responseText)))
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, responseText)
}

func (s *Server) valueHandler(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")

	s.storage.mu.RLock()
	defer s.storage.mu.RUnlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	switch metricType {
	case "gauge":
		value, exists := s.storage.gauges[metricName]
		if !exists {
			http.Error(w, "Metric not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%g", value)

	case "counter":
		value, exists := s.storage.counters[metricName]
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

func (s *Server) rootHandler(w http.ResponseWriter, r *http.Request) {
	// Создаем копии для безопасной работы с шаблоном
	gaugesCopy := make(map[string]float64)
	countersCopy := make(map[string]int64)

	// Блокируем только на время копирования
	s.storage.mu.RLock()
	for k, v := range s.storage.gauges {
		gaugesCopy[k] = v
	}
	for k, v := range s.storage.counters {
		countersCopy[k] = v
	}
	s.storage.mu.RUnlock()

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

func main() {
	if err := run(); err != nil {
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := parseServerFlags()
	if err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	// Создаем зависимости
	storage := NewMetricsStorage()
	server := NewServer(storage, config)

	log.Printf("Starting metrics server on %s", config.Address)
	if err := http.ListenAndServe(config.Address, server.Router()); err != nil {
		return fmt.Errorf("server failed to start: %w", err)
	}

	return nil
}

func parseServerFlags() (*ServerConfig, error) {
	config := &ServerConfig{}

	flag.StringVar(&config.Address, "a", "localhost:8080", "HTTP server endpoint address")
	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Error: unknown arguments: %v\n", flag.Args())
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		flag.PrintDefaults()
		return nil, fmt.Errorf("unknown arguments provided")
	}

	return config, nil
}
