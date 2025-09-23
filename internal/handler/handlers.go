package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"github.com/kvsukharev/go-musthave-metrics-tpl/internal/storage"

	"github.com/go-chi/chi/v5"
)

type MetricHandlers struct {
	storage storage.Storage
}

func NewMetricHandlers(storage storage.Storage) *MetricHandlers {
	return &MetricHandlers{storage: storage}
}

// updateHandler обрабатывает запросы на обновление метрик
func (h *MetricHandlers) updateHandler(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")
	metricValue := chi.URLParam(r, "value")

	switch metricType {
	case "gauge":
		value, err := strconv.ParseFloat(metricValue, 64)
		if err != nil {
			http.Error(w, "Invalid gauge value", http.StatusBadRequest)
			return
		}
		h.storage.UpdateGauge(metricName, value)
		log.Printf("Updated gauge %s = %.6f", metricName, value)

	case "counter":
		value, err := strconv.ParseInt(metricValue, 10, 64)
		if err != nil {
			http.Error(w, "Invalid counter value", http.StatusBadRequest)
			return
		}
		h.storage.UpdateCounter(metricName, value)
		log.Printf("Updated counter %s (added %d)", metricName, value)

	default:
		http.Error(w, "Unknown metric type. Use 'gauge' or 'counter'", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK\n")
}

// valueHandler обрабатывает запросы на получение значения метрики
func (h *MetricHandlers) valueHandler(w http.ResponseWriter, r *http.Request) {
	metricType := chi.URLParam(r, "type")
	metricName := chi.URLParam(r, "name")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	switch metricType {
	case "gauge":
		value, err := h.storage.GetGauge(metricName)
		if err != nil {
			http.Error(w, "Metric not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%g", value)

	case "counter":
		value, err := h.storage.GetCounter(metricName)
		if err != nil {
			http.Error(w, "Metric not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%d", value)

	default:
		http.Error(w, "Unknown metric type. Use 'gauge' or 'counter'", http.StatusBadRequest)
	}
}

// rootHandler отображает HTML-страницу со всеми метриками
func (h *MetricHandlers) rootHandler(w http.ResponseWriter, r *http.Request) {
	gauges, counters := h.storage.GetAllMetrics()

	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>Metrics</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
        h1 { color: #333; }
    </style>
</head>
<body>
    <h1>Metrics Dashboard</h1>
    
    <h2>Gauges</h2>
    <table>
        <tr><th>Name</th><th>Value</th></tr>
        {{range $name, $value := .Gauges}}
        <tr><td>{{$name}}</td><td>{{$value}}</td></tr>
        {{else}}
        <tr><td colspan="2">No gauges available</td></tr>
        {{end}}
    </table>
    
    <h2>Counters</h2>
    <table>
        <tr><th>Name</th><th>Value</th></tr>
        {{range $name, $value := .Counters}}
        <tr><td>{{$name}}</td><td>{{$value}
        {{range $name, $value := .Counters}}
        <tr><td>{{$name}}</td><td>{{$value}}</td></tr>
        {{else}}
        <tr><td colspan="2">No counters available</td></tr>
        {{end}}
    </table>
</body>
</html>`

	t, err := template.New("metrics").Parse(tmpl)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Gauges   map[string]float64
		Counters map[string]int64
	}{
		Gauges:   gauges,
		Counters: counters,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := t.Execute(w, data); err != nil {
		log.Printf("Template execution error: %v", err)
	}
}
