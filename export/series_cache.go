// Copyright 2020 Google Inc.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package export

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/textparse"
	"github.com/prometheus/prometheus/scrape"
	metric_pb "google.golang.org/genproto/googleapis/api/metric"
	monitoredres_pb "google.golang.org/genproto/googleapis/api/monitoredres"
	monitoring_pb "google.golang.org/genproto/googleapis/monitoring/v3"
)

type seriesStore interface {
	// Same interface as the standard map getter.
	get(ref uint64, target *scrape.Target) (*seriesCacheEntry, bool, error)

	// Get the reset timestamp and adjusted value for the input sample.
	// If false is returned, the sample should be skipped.
	getResetAdjusted(ref uint64, t int64, v float64) (int64, float64, bool)

	// Attempt to set the new most recent time range for the series with given hash.
	// Returns false if it failed, in which case the sample must be discarded.
	updateSampleInterval(hash uint64, start, end int64) bool
}

// seriesCache holds a mapping from series reference to label set.
// It can garbage collect obsolete entries based on the most recent WAL checkpoint.
// Implements seriesGetter.
type seriesCache struct {
	logger        log.Logger
	metricsPrefix string

	// Guards access to the entries and intervals maps and the lastRefresh
	// field of individual cache entries.
	mtx sync.Mutex
	// Map from series reference to various cached information about it.
	entries map[uint64]*seriesCacheEntry
	// Map from series hash to most recently written interval.
	intervals map[uint64]sampleInterval

	// Function to retrieve a label set for a series reference number.
	// Returns nil if the reference is no longer valid.
	getLabelsByRef func(uint64) labels.Labels

	// Function to retrieve external labels for the instance.
	getExternalLabels func() labels.Labels
}

type seriesCacheEntry struct {
	// The uniquely identifying set of labels for the series.
	lset labels.Labels

	// Metadata for the metric of the series.
	metadata scrape.MetricMetadata
	// A pre-populated time protobuf to be sent to the GCM API. It can
	// be shallow-copied and populated with point values to avoid excessive
	// allocations for each datapoint exported for the series.
	proto *monitoring_pb.TimeSeries
	// The well-known Prometheus metric name suffix if any.
	suffix metricSuffix
	// A hash of the series descriptor.
	hash uint64
	// Timestamp after which to refresh the cached state.
	nextRefresh int64

	// Tracked counter reset state for conversion to GCM cumulatives.
	hasReset       bool
	resetValue     float64
	resetTimestamp int64
}

const (
	refreshInterval = 10 * time.Minute
	refreshJitter   = 10 * time.Minute
)

// valid returns true if the Prometheus series can be converted to a GCM series.
func (e *seriesCacheEntry) valid() bool {
	return e.proto != nil
}

// shouldRefresh returns true if the cached state should be refreshed.
func (e *seriesCacheEntry) shouldRefresh() bool {
	return time.Now().Unix() > e.nextRefresh
}

// setNextRefresh determines a timestamp for the next refresh.
func (e *seriesCacheEntry) setNextRefresh() {
	// Randomly offset the timestamp around the targeted average so a bulk of simultaniously
	// created series are not invalidated all at once, causing potential CPU and allocation
	// spikes.
	jitter := time.Duration((rand.Float64() - 0.5) * float64(refreshJitter))
	e.nextRefresh = time.Now().Add(refreshInterval).Add(jitter).Unix()
}

func newSeriesCache(logger log.Logger, metricsPrefix string, getExternalLabels func() labels.Labels) *seriesCache {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &seriesCache{
		logger:            logger,
		metricsPrefix:     metricsPrefix,
		entries:           map[uint64]*seriesCacheEntry{},
		intervals:         map[uint64]sampleInterval{},
		getExternalLabels: getExternalLabels,
	}
}

func (c *seriesCache) run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := c.garbageCollect(); err != nil {
				level.Error(c.logger).Log("msg", "garbage collection failed", "err", err)
			}
		}
	}
}

// invalidateAll invalidates all cache entries.
func (c *seriesCache) invalidateAll() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	// Set next refresh to the zero timestamp to trigger a refresh.
	for _, e := range c.entries {
		e.nextRefresh = 0
	}
}

// garbageCollect drops obsolete cache entries based on the contents of the most
// recent checkpoint.
func (c *seriesCache) garbageCollect() error {
	level.Debug(c.logger).Log("msg", "garbage collection not implemented yet")
	return nil
}

func (c *seriesCache) get(ref uint64, target *scrape.Target) (*seriesCacheEntry, bool, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	e, ok := c.entries[ref]

	if !ok {
		lset := c.getLabelsByRef(ref)
		if lset == nil {
			return nil, false, errors.New("series reference invalid")
		}
		e = &seriesCacheEntry{lset: lset}

		c.entries[ref] = e
	}
	if e.shouldRefresh() {
		if err := c.populate(e, target); err != nil {
			return nil, false, err
		}
		e.setNextRefresh()
	}
	return e, e.valid(), nil
}

// updateSampleInterval attempts to set the new most recent time range for the series with given hash.
// Returns false if it failed, in which case the sample must be discarded.
func (c *seriesCache) updateSampleInterval(hash uint64, start, end int64) bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	iv, ok := c.intervals[hash]
	if !ok || iv.accepts(start, end) {
		c.intervals[hash] = sampleInterval{start, end}
		return true
	}
	return false
}

type sampleInterval struct {
	start, end int64
}

func (si *sampleInterval) accepts(start, end int64) bool {
	return (start == si.start && end > si.end) || (start > si.start && start >= si.end)
}

// getResetAdjusted takes a sample for a referenced series and returns
// its reset timestamp and adjusted value.
// If the last return argument is false, the sample should be dropped.
func (c *seriesCache) getResetAdjusted(ref uint64, t int64, v float64) (int64, float64, bool) {
	c.mtx.Lock()
	e, ok := c.entries[ref]
	c.mtx.Unlock()
	if !ok {
		return 0, 0, false
	}
	hasReset := e.hasReset
	e.hasReset = true
	if !hasReset {
		e.resetTimestamp = t
		e.resetValue = v
		// If we just initialized the reset timestamp, this sample should be skipped.
		// We don't know the window over which the current cumulative value was built up over.
		// The next sample for will be considered from this point onwards.
		return 0, 0, false
	}
	if v < e.resetValue {
		// If the series was reset, set the reset timestamp to be one millisecond
		// before the timestamp of the current sample.
		// We don't know the true reset time but this ensures the range is non-zero
		// while unlikely to conflict with any previous sample.
		e.resetValue = 0
		e.resetTimestamp = t - 1
	}
	return e.resetTimestamp, v - e.resetValue, true
}

// populate cached state for the given entry.
func (c *seriesCache) populate(entry *seriesCacheEntry, target *scrape.Target) error {
	// Break the series into resource and metric labels.
	resource, metricLabels, ok := c.extractResource(entry.lset)
	if !ok {
		level.Debug(c.logger).Log("msg", "unknown resource", "series", entry.lset)
		return nil
	}

	// Remove the __name__ label as it becomes the metric type in the GCM time series.
	for i, l := range metricLabels {
		if l.Name == "__name__" {
			metricLabels = append(metricLabels[:i], metricLabels[i+1:]...)
			break
		}
	}
	// Drop series with too many labels.
	// TODO: remove once field limit is lifted in the GCM API.
	if len(metricLabels) > maxLabelCount {
		level.Debug(c.logger).Log("msg", "too many labels", "labels", metricLabels)
		return nil
	}

	var (
		metricName     = entry.lset.Get("__name__")
		baseMetricName string
		suffix         metricSuffix
	)
	metadata, ok := getMetadata(target, metricName)
	if !ok {
		// The full name didn't turn anything up. Check again in case it's a summary,
		// histogram, or counter without the metric name suffix.
		var ok bool
		if baseMetricName, suffix, ok = splitComplexMetricSuffix(metricName); ok {
			metadata, ok = getMetadata(target, baseMetricName)
		}
		if !ok {
			level.Debug(c.logger).Log("msg", "metadata not found", "metric_name", metricName)
			return nil
		}
	}
	// Handle label modifications for histograms early so we don't build the label map twice.
	// We have to remove the 'le' label which defines the bucket boundary.
	if metadata.Type == textparse.MetricTypeHistogram {
		for i, l := range metricLabels {
			if l.Name == "le" {
				metricLabels = append(metricLabels[:i], metricLabels[i+1:]...)
				break
			}
		}
	}

	ts := &monitoring_pb.TimeSeries{
		Metric: &metric_pb.Metric{
			Type:   c.getMetricType(metricName),
			Labels: metricLabels.Map(),
		},
		Resource: resource,
	}

	// TODO: handle untyped.
	switch metadata.Type {
	case textparse.MetricTypeCounter:
		ts.MetricKind = metric_pb.MetricDescriptor_CUMULATIVE
		ts.ValueType = metric_pb.MetricDescriptor_DOUBLE
	case textparse.MetricTypeGauge, textparse.MetricTypeUnknown:
		ts.MetricKind = metric_pb.MetricDescriptor_GAUGE
		ts.ValueType = metric_pb.MetricDescriptor_DOUBLE
	case textparse.MetricTypeSummary:
		switch suffix {
		case metricSuffixSum:
			ts.MetricKind = metric_pb.MetricDescriptor_CUMULATIVE
			ts.ValueType = metric_pb.MetricDescriptor_DOUBLE
		case metricSuffixCount:
			ts.MetricKind = metric_pb.MetricDescriptor_CUMULATIVE
			ts.ValueType = metric_pb.MetricDescriptor_INT64
		case metricSuffixNone: // Actual quantiles.
			ts.MetricKind = metric_pb.MetricDescriptor_GAUGE
			ts.ValueType = metric_pb.MetricDescriptor_DOUBLE
		default:
			return errors.Errorf("unexpected metric name suffix %q", suffix)
		}
	case textparse.MetricTypeHistogram:
		ts.Metric.Type = c.getMetricType(baseMetricName)
		ts.MetricKind = metric_pb.MetricDescriptor_CUMULATIVE
		ts.ValueType = metric_pb.MetricDescriptor_DISTRIBUTION
	default:
		return errors.Errorf("unexpected metric type %s", metadata.Type)
	}

	entry.proto = ts
	entry.metadata = metadata
	entry.suffix = suffix
	entry.hash = hashSeries(ts)

	return nil
}

func (c *seriesCache) getMetricType(name string) string {
	return getMetricType(c.metricsPrefix, name)
}

// getResource returns the monitored resource, the entry labels, and whether the operation succeeded.
// The returned entry labels are a subset of `lset` without the labels that were used as resource labels.
func (c *seriesCache) extractResource(lset labels.Labels) (*monitoredres_pb.MonitoredResource, labels.Labels, bool) {
	// Drop series that don't contain required job/instance labels. This will affect metrics written
	// from recording or alerting rules.
	// TODO(freinartz): consider supporting these in the backend. All fields other then location
	// and cluster being empty should be sensible as cardinality shouldn't be high for these.
	if !lset.Has(keyJob) {
		return nil, nil, false
	}
	if !lset.Has(keyInstance) {
		return nil, nil, false
	}
	// TOOD(freinartz): consider checking whether the target comes from Kubernetes service
	// discovery and a namespace can be inferred from discovery metadata. This could help populate
	// the schema correctly for Prometheus configs that don't relabel the namespace properly
	// without requiring user action.
	// It may also introduce unexpected behavior though, so this needs to be evaluated in depth.

	// Prometheus allows to configure external labels, which are attached when exporting data out of
	// the instance to disambiguate data across instances. For us they generally include 'location' and 'cluster'.
	// Per Prometheus semantics they are merged into lset, while lset takes precedence on collisions.
	//
	// This generally seems problematic as it violates hierarchical precedence. Especially "location"
	// being overwritten from a metric label would likely result the in the data being rejected.
	// A sensible solution could be to adopt Prometheus collision resolution for target and metric
	// labels, in which colliding metric label keys are prefixed with 'exported_'.
	// But until we've a user issue caused by this we stay true to the upstream semantics.
	builder := labels.NewBuilder(lset)

	for _, l := range c.getExternalLabels() {
		if !lset.Has(l.Name) {
			builder.Set(l.Name, l.Value)
		}
	}
	lset = builder.Labels()

	mres := &monitoredres_pb.MonitoredResource{
		Type: "prometheus_target",
		Labels: map[string]string{
			keyLocation:  lset.Get(keyLocation),
			keyCluster:   lset.Get(keyCluster),
			keyNamespace: lset.Get(keyNamespace),
			keyJob:       lset.Get(keyJob),
			keyInstance:  lset.Get(keyInstance),
		},
	}
	builder.Del(keyLocation)
	builder.Del(keyCluster)
	builder.Del(keyNamespace)
	builder.Del(keyJob)
	builder.Del(keyInstance)

	return mres, builder.Labels(), true
}

// Metrics Prometheus writes at scrape time for which no metadata is exposed.
var internalMetrics = map[string]scrape.MetricMetadata{
	"up": {
		Metric: "up",
		Type:   textparse.MetricTypeGauge,
		Help:   "Up indicates whether the last target scrape was successful.",
	},
	"scrape_samples_scraped": {
		Metric: "scrape_samples_scraped",
		Type:   textparse.MetricTypeGauge,
		Help:   "How many samples were scraped during the last successful scrape.",
	},
	"scrape_duration_seconds": {
		Metric: "scrape_duration_seconds",
		Type:   textparse.MetricTypeGauge,
		Help:   "Duration of the last scrape.",
	},
	"scrape_samples_post_metric_relabeling": {
		Metric: "scrape_samples_post_metric_relabeling",
		Type:   textparse.MetricTypeGauge,
		Help:   "How many samples were ingested after relabeling.",
	},
	"scrape_series_added": {
		Metric: "scrape_series_added",
		Type:   textparse.MetricTypeGauge,
		Help:   "Number of new series added in the last scrape.",
	},
}

// getMetadata retrieves metric metadata for its scraped metrics or synthetic
// metrics recorded about the scrape itself.
func getMetadata(target *scrape.Target, metric string) (scrape.MetricMetadata, bool) {
	if md, ok := target.Metadata(metric); ok {
		return md, true
	}
	md, ok := internalMetrics[metric]
	return md, ok
}

func hashSeries(s *monitoring_pb.TimeSeries) uint64 {
	const sep = '\xff'
	h := hashNew()

	h = hashAdd(h, s.Resource.Type)
	h = hashAddByte(h, sep)
	h = hashAdd(h, s.Metric.Type)

	// Map iteration is randomized. We thus convert the labels to sorted slices
	// with labels.FromMap before hashing.
	for _, l := range labels.FromMap(s.Resource.Labels) {
		h = hashAddByte(h, sep)
		h = hashAdd(h, l.Name)
		h = hashAddByte(h, sep)
		h = hashAdd(h, l.Value)
	}
	h = hashAddByte(h, sep)
	for _, l := range labels.FromMap(s.Metric.Labels) {
		h = hashAddByte(h, sep)
		h = hashAdd(h, l.Name)
		h = hashAddByte(h, sep)
		h = hashAdd(h, l.Value)
	}
	return h
}
