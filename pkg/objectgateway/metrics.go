package objectgateway

import (
	"context"
	"net/http"
	"strconv"
	"time"

	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/smithy-go/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// The gateway's own numbers for the monitoring stack (storage plan step
// 5): what consumers ask and how it ends, by listener, method and status
// class; what the provider takes to answer, by operation. Nothing is
// labelled by bucket: at a hundred thousand projects that is a hundred
// thousand series, and per-bucket counts live in the descriptors.
var (
	registry = prometheus.NewRegistry()

	requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "shpyrd_gateway_requests_total",
		Help: "S3 requests the gateway answered, by listener, method and status class.",
	}, []string{"listener", "method", "status"})
	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "shpyrd_gateway_request_duration_seconds",
		Help:    "How long an S3 request took to answer, by listener and method.",
		Buckets: []float64{.005, .01, .025, .05, .1, .2, .5, 1, 2.5, 5, 10, 30},
	}, []string{"listener", "method"})
	providerDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "shpyrd_gateway_provider_request_duration_seconds",
		Help:    "How long the provider took to answer one call, by operation.",
		Buckets: []float64{.005, .01, .025, .05, .1, .2, .5, 1, 2.5, 5, 10, 30},
	}, []string{"operation"})
	providerErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "shpyrd_gateway_provider_errors_total",
		Help: "Provider calls that failed, by operation.",
	}, []string{"operation"})
	capRefusals = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "shpyrd_gateway_cap_refusals_total",
		Help: "Uploads refused because the bucket would pass its cap.",
	})
	slowDowns = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "shpyrd_gateway_slowdowns_total",
		Help: "Requests answered SlowDown by the per-bucket rate limit.",
	})
	staleAborts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "shpyrd_gateway_stale_uploads_aborted_total",
		Help: "Multipart uploads the sweep aborted for being older than seven days.",
	})
)

func init() {
	registry.MustRegister(requests, requestDuration, providerDuration, providerErrors, capRefusals, slowDowns, staleAborts)
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// MetricsHandler serves the registry in the Prometheus text format.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// observeRequests is the Fiber middleware that counts and times every
// request of one listener.
func observeRequests(listener string) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		if err != nil && status < 400 {
			status = http.StatusInternalServerError
		}
		method := c.Method()
		requests.WithLabelValues(listener, method, strconv.Itoa(status/100)+"xx").Inc()
		requestDuration.WithLabelValues(listener, method).Observe(time.Since(start).Seconds())
		return err
	}
}

// observeProvider times every call the provider client makes, each
// attempt on its own.
func observeProvider(stack *middleware.Stack) error {
	return stack.Finalize.Add(middleware.FinalizeMiddlewareFunc("shpyrdObserve", func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
		start := time.Now()
		out, md, err := next.HandleFinalize(ctx, in)
		op := awsmiddleware.GetOperationName(ctx)
		providerDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
		if err != nil {
			providerErrors.WithLabelValues(op).Inc()
		}
		return out, md, err
	}), middleware.After)
}
