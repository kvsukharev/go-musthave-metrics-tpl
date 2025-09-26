package agent

import (
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	collector := NewCollector()

	if collector == nil {
		t.Fatal("NewCollector() returned nil")
	}

	if collector.gauge == nil {
		t.Error("gauges map is nil")
	}

	if collector.counter == nil {
		t.Error("counters map is nil")
	}
}

func TestUpdateMetrics(t *testing.T) {
	collector := NewCollector()

	// Проверяем начальное состояние
	gauge := collector.GetGauges()
	counter := collector.GetCounters()

	if len(gauge) != 0 {
		t.Errorf("Expected 0 initial gauges, got %d", len(gauge))
	}

	if len(counter) != 0 {
		t.Errorf("Expected 0 initial counters, got %d", len(counter))
	}

	// Обновляем метрики
	collector.UpdateMetrics()

	gauge = collector.GetGauges()
	counter = collector.GetCounters()

	// Проверяем что метрики собрались
	if len(gauge) == 0 {
		t.Error("No gauge metrics collected")
	}

	if len(counter) == 0 {
		t.Error("No counter metrics collected")
	}

	// Проверяем наличие обязательных метрик
	if _, exists := gauge["RandomValue"]; !exists {
		t.Error("RandomValue gauge metric not found")
	}

	if pollCount, exists := counter["PollCount"]; !exists {
		t.Error("PollCount counter metric not found")
	} else if pollCount != 1 {
		t.Errorf("Expected PollCount = 1, got %d", pollCount)
	}

	// Проверяем некоторые runtime метрики
	requiredGauges := []string{
		"Alloc", "Sys", "HeapAlloc", "HeapSys", "NumGC", "GCCPUFraction",
	}

	for _, name := range requiredGauges {
		if _, exists := gauge[name]; !exists {
			t.Errorf("Required gauge metric %s not found", name)
		}
	}
}

func TestPollCountIncrement(t *testing.T) {
	collector := NewCollector()

	// Обновляем метрики несколько раз
	for i := 1; i <= 5; i++ {
		collector.UpdateMetrics()

		counter := collector.GetCounters()
		pollCount := counter["PollCount"]

		if pollCount != int64(i) {
			t.Errorf("After %d updates, expected PollCount = %d, got %d",
				i, i, pollCount)
		}
	}
}

func TestRandomValueChanges(t *testing.T) {
	collector := NewCollector()

	collector.UpdateMetrics()
	gauge1 := collector.GetGauges()
	randomValue1 := gauge1["RandomValue"]

	// Небольшая пауза для изменения seed времени
	time.Sleep(1 * time.Millisecond)

	// RandomValue должно изменяться (хотя теоретически может совпасть)
	// Проверим несколько раз для уверенности
	different := false
	for i := 0; i < 10; i++ {
		collector.UpdateMetrics()
		gauges := collector.GetGauges()
		if gauges["RandomValue"] != randomValue1 {
			different = true
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	if !different {
		t.Error("RandomValue should change between updates")
	}
}

func TestConcurrentAccess(t *testing.T) {
	collector := NewCollector()

	done := make(chan bool)

	// Горутина для записи
	go func() {
		for i := 0; i < 100; i++ {
			collector.UpdateMetrics()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Горутина для чтения
	go func() {
		for i := 0; i < 100; i++ {
			_ = collector.GetGauges()
			_ = collector.GetCounters()
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Ждем завершения обеих горутин
	<-done
	<-done

	// Проверяем финальное состояние
	counters := collector.GetCounters()
	if counters["PollCount"] != 100 {
		t.Errorf("Expected PollCount = 100, got %d", counters["PollCount"])
	}
}
