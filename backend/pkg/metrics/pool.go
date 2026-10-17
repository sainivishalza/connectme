package metrics

import (
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// current is the pool the pool gauges read. Registering the gauges once and
// swapping the pool lets tests (and restarts in-process) call RegisterPool
// repeatedly without duplicate-registration panics.
var current atomic.Pointer[pgxpool.Pool]

// RegisterPool points the connection-pool gauges at p.
func RegisterPool(p *pgxpool.Pool) { current.Store(p) }

func poolGauge(name, help string, f func(*pgxpool.Stat) float64) {
	Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, func() float64 {
		p := current.Load()
		if p == nil {
			return 0
		}
		return f(p.Stat())
	}))
}

func init() {
	poolGauge("connectme_db_pool_acquired_conns", "Connections currently in use.",
		func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) })
	poolGauge("connectme_db_pool_idle_conns", "Idle connections.",
		func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) })
	poolGauge("connectme_db_pool_max_conns", "Pool size limit.",
		func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) })
	poolGauge("connectme_db_pool_empty_acquire_total", "Acquires that had to wait for a free connection.",
		func(s *pgxpool.Stat) float64 { return float64(s.EmptyAcquireCount()) })
}
