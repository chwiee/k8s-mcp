package app

import "github.com/prometheus/client_golang/prometheus"

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "k8smcp_requests_total",
			Help: "Total number of requests handled by the k8s-mcp server.",
		},
		[]string{"action", "status"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "k8smcp_request_duration_seconds",
			Help:    "Duration of solve requests in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"action"},
	)
	activeRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "k8smcp_active_requests",
			Help: "Number of in-flight requests currently being processed.",
		},
	)
	knowledgeBaseDocuments = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "k8smcp_knowledge_base_documents",
			Help: "Number of markdown files currently loaded from the knowledge base directory.",
		},
	)
	knowledgeBaseReloadUnix = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "k8smcp_knowledge_base_last_reload_timestamp_seconds",
			Help: "Unix timestamp of the last successful knowledge base reload.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		requestsTotal,
		requestDuration,
		activeRequests,
		knowledgeBaseDocuments,
		knowledgeBaseReloadUnix,
	)
}
