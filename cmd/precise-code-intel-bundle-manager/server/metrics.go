package server

// func init() {
// 	prometheus.MustRegister(bundlesRemoved)
// }

// var bundlesRemoved = prometheus.NewCounter(prometheus.CounterOpts{
// 	Namespace: "src",
// 	Subsystem: "precise-code-intel-bundle-manager",
// 	Name:      "bundles_removed",
// 	Help:      "number of bundles removed during cleanup",
// })

// var execRunning = prometheus.NewGaugeVec(prometheus.GaugeOpts{
// 	Namespace: "src",
// 	Subsystem: "gitserver",
// 	Name:      "exec_running",
// 	Help:      "number of gitserver.Command running concurrently.",
// }, []string{"cmd", "repo"})

// var execDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
// 	Namespace: "src",
// 	Subsystem: "gitserver",
// 	Name:      "exec_duration_seconds",
// 	Help:      "gitserver.Command latencies in seconds.",
// 	Buckets:   trace.UserLatencyBuckets,
// }, []string{"cmd", "repo", "status"})

// var cloneQueue = prometheus.NewGauge(prometheus.GaugeOpts{
// 	Namespace: "src",
// 	Subsystem: "gitserver",
// 	Name:      "clone_queue",
// 	Help:      "number of repos waiting to be cloned.",
// })

// func registerPrometheusCollector(db *sql.DB, dbNameSuffix string) {
// 	c := prometheus.NewGaugeFunc(
// 		prometheus.GaugeOpts{
// 			Namespace: "src",
// 			Subsystem: "pgsql" + dbNameSuffix,
// 			Name:      "open_connections",
// 			Help:      "Number of open connections to pgsql DB, as reported by pgsql.DB.Stats()",
// 		},
// 		func() float64 {
// 			s := db.Stats()
// 			return float64(s.OpenConnections)
// 		},
// 	)
// 	prometheus.MustRegister(c)
// }

// var (
// 	fetching = prometheus.NewGauge(prometheus.GaugeOpts{
// 		Namespace: "symbols",
// 		Subsystem: "store",
// 		Name:      "fetching",
// 		Help:      "The number of fetches currently running.",
// 	})
// 	fetchQueueSize = prometheus.NewGauge(prometheus.GaugeOpts{
// 		Namespace: "symbols",
// 		Subsystem: "store",
// 		Name:      "fetch_queue_size",
// 		Help:      "The number of fetch jobs enqueued.",
// 	})
// 	fetchFailed = prometheus.NewCounter(prometheus.CounterOpts{
// 		Namespace: "symbols",
// 		Subsystem: "store",
// 		Name:      "fetch_failed",
// 		Help:      "The total number of archive fetches that failed.",
// 	})
// )

// func init() {
// 	prometheus.MustRegister(fetching)
// 	prometheus.MustRegister(fetchQueueSize)
// 	prometheus.MustRegister(fetchFailed)
// }
