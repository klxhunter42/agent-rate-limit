package middleware

import (
	"context"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// RuntimeMetrics holds Prometheus gauges for Go runtime and Dragonfly health.
type RuntimeMetrics struct {
	goroutines  prometheus.Gauge
	heapAlloc   prometheus.Gauge
	heapObjects prometheus.Gauge
	gcPauseNs   prometheus.Gauge
	stackInUse  prometheus.Gauge
	dragonflyUp prometheus.Gauge
}

// NewRuntimeMetrics registers all runtime Prometheus gauges on the given registerer.
func NewRuntimeMetrics(reg prometheus.Registerer) *RuntimeMetrics {
	return &RuntimeMetrics{
		goroutines: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway",
			Name:      "go_goroutines",
			Help:      "Current number of goroutines.",
		}),
		heapAlloc: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway",
			Name:      "go_heap_alloc_bytes",
			Help:      "Current heap allocation in bytes.",
		}),
		heapObjects: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway",
			Name:      "go_heap_objects",
			Help:      "Current number of heap objects.",
		}),
		gcPauseNs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway",
			Name:      "go_gc_pause_ns",
			Help:      "GC pause of last cycle in nanoseconds.",
		}),
		stackInUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway",
			Name:      "go_stack_inuse_bytes",
			Help:      "Current stack in-use in bytes.",
		}),
		dragonflyUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway",
			Name:      "dragonfly_up",
			Help:      "Dragonfly/Redis health (1=healthy, 0=down).",
		}),
	}
}

// MustRegister registers all gauges on the given registerer.
func (rm *RuntimeMetrics) MustRegister(reg prometheus.Registerer) {
	reg.MustRegister(
		rm.goroutines,
		rm.heapAlloc,
		rm.heapObjects,
		rm.gcPauseNs,
		rm.stackInUse,
		rm.dragonflyUp,
	)
}

// Start launches a background goroutine that collects runtime and Dragonfly metrics every 10 seconds.
func (rm *RuntimeMetrics) Start(ctx context.Context, redisAddr string) {
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			rdb.Close()
			return
		case <-ticker.C:
			rm.collect(rdb, ctx)
		}
	}
}

func (rm *RuntimeMetrics) collect(rdb *redis.Client, ctx context.Context) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	rm.goroutines.Set(float64(runtime.NumGoroutine()))
	rm.heapAlloc.Set(float64(ms.HeapAlloc))
	rm.heapObjects.Set(float64(ms.HeapObjects))
	rm.stackInUse.Set(float64(ms.StackInuse))

	lastPause := ms.PauseNs[(ms.NumGC+255)%256]
	rm.gcPauseNs.Set(float64(lastPause))

	if err := rdb.Ping(ctx).Err(); err != nil {
		rm.dragonflyUp.Set(0)
	} else {
		rm.dragonflyUp.Set(1)
	}
}
