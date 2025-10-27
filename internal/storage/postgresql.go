package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStorage struct {
	pool *pgxpool.Pool
}

func (p *PostgresStorage) Close() {
	panic("unimplemented")
}

func NewPostgresStorage(ctx context.Context, dsn string) (*PostgresStorage, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	return &PostgresStorage{pool: pool}, nil
}

func (p *PostgresStorage) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Пример реализации метода для счетчиков
func (p *PostgresStorage) UpdateCounter(ctx context.Context, name string, value int64) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO counters (name, value)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE
		SET value = counters.value + EXCLUDED.value`,
		name, value)
	return err
}

func (p *PostgresStorage) UpdateGauge(ctx context.Context, name string, value float64) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO gauges (name, value)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE
		SET value = EXCLUDED.value`,
		name, value)
	return err
}

func (p *PostgresStorage) GetAllMetrics(ctx context.Context) (map[string]float64, map[string]int64, error) {
	gauges := make(map[string]float64)
	counters := make(map[string]int64)

	// Получение gauge метрик
	rows, err := p.pool.Query(ctx, "SELECT name, value FROM gauges")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var value float64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, nil, err
		}
		gauges[name] = value
	}

	// Получение counter метрик
	rows, err = p.pool.Query(ctx, "SELECT name, value FROM counters")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var value int64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, nil, err
		}
		counters[name] = value
	}

	return gauges, counters, nil
}
