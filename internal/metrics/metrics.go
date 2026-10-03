// Package metrics defines the Prometheus metrics exported on /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "converseai_http_request_duration_seconds",
		Help:    "HTTP request latency by route and status.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method", "status"})

	LLMCallDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "converseai_llm_call_duration_seconds",
		Help:    "Latency of model calls by model and purpose.",
		Buckets: []float64{0.5, 1, 2, 5, 10, 20, 40, 80, 160, 320},
	}, []string{"model", "kind", "outcome"})

	LLMTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "converseai_llm_tokens_total",
		Help: "Tokens processed, by model and direction (prompt|completion).",
	}, []string{"model", "direction"})

	ToolCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "converseai_tool_calls_total",
		Help: "Agent tool calls by tool and outcome (ok|error|invalid_args).",
	}, []string{"tool", "outcome"})

	RunStageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "converseai_run_stage_duration_seconds",
		Help:    "Duration of each stage of a chat run.",
		Buckets: []float64{0.05, 0.2, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
	}, []string{"stage"})

	RunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "converseai_runs_total",
		Help: "Chat runs by final status.",
	}, []string{"status"})

	RunQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "converseai_run_queue_depth",
		Help: "Runs waiting for a free execution slot.",
	})

	ModelQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "converseai_model_queue_depth",
		Help: "Requests waiting for the GPU to switch models.",
	})

	ModelWaitSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "converseai_model_wait_seconds",
		Help:    "Time spent waiting for a model lease.",
		Buckets: []float64{0.001, 0.01, 0.1, 1, 5, 15, 60, 180},
	})

	RateLimited = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "converseai_rate_limited_total",
		Help: "Requests rejected by the rate limiter.",
	}, []string{"limiter"})
)
