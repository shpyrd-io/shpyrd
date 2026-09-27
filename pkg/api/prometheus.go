package api

import "github.com/shpyrd-io/shpyrd/pkg/prom"

// PromClient is the Prometheus HTTP client (re-exported from pkg/prom
// so api callers do not need to import pkg/prom directly).
type PromClient = prom.Client
type Point = prom.Point
type RawSeries = prom.RawSeries

// NewPromClient returns a client for baseURL.
func NewPromClient(baseURL string) *PromClient { return prom.NewClient(baseURL) }
