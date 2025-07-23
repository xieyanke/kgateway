package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kgateway-dev/kgateway/v2/pkg/logging"
	"github.com/kgateway-dev/kgateway/v2/pkg/metrics"
)

const (
	translatorSubsystem = "translator"
	translatorNameLabel = "translator"
	resourcesSubsystem  = "resources"
	snapshotSubsystem   = "xds_snapshot"
)

var logger = logging.New("translator.metrics")

var (
	translationHistogramBuckets = []float64{0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}
	translationsTotal           = metrics.NewCounter(
		metrics.CounterOpts{
			Subsystem: translatorSubsystem,
			Name:      "translations_total",
			Help:      "Total number of translations",
		},
		[]string{translatorNameLabel, "result"},
	)
	translationDuration = metrics.NewHistogram(
		metrics.HistogramOpts{
			Subsystem:                       translatorSubsystem,
			Name:                            "translation_duration_seconds",
			Help:                            "Translation duration",
			Buckets:                         translationHistogramBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		},
		[]string{translatorNameLabel},
	)
	translationsRunning = metrics.NewGauge(
		metrics.GaugeOpts{
			Subsystem: translatorSubsystem,
			Name:      "translations_running",
			Help:      "Current number of translations running",
		},
		[]string{translatorNameLabel},
	)

	xdsSnapshotSyncsTotal = metrics.NewCounter(metrics.CounterOpts{
		Subsystem: snapshotSubsystem,
		Name:      "syncs_total",
		Help:      "Total number of XDS snapshot syncs",
	},
		[]string{"gateway", "namespace"})

	resourcesSyncsStartedTotal = metrics.NewCounter(metrics.CounterOpts{
		Subsystem: resourcesSubsystem,
		Name:      "syncs_started_total",
		Help:      "Total number of syncs started",
	},
		[]string{"gateway", "namespace", "resource"})
	resourcesUpdatesDroppedTotal = metrics.NewCounter(metrics.CounterOpts{
		Subsystem: resourcesSubsystem,
		Name:      "updates_dropped_total",
		Help:      "Total number of resources metrics updates dropped. If this metric is ever greater than 0, all resources subsystem metrics should be considered invalid until process restart",
	}, nil)
)

type ResourceMetricLabels struct {
	Gateway   string
	Namespace string
	Resource  string
}

func (r ResourceMetricLabels) toMetricsLabels() []metrics.Label {
	return []metrics.Label{
		{Name: "gateway", Value: r.Gateway},
		{Name: "namespace", Value: r.Namespace},
		{Name: "resource", Value: r.Resource},
	}
}

// TranslatorMetricsRecorder defines the interface for recording translator metrics.
type TranslatorMetricsRecorder interface {
	TranslationStart() func(error)
}

// translatorMetrics records metrics for translator operations.
type translatorMetrics struct {
	translatorName      string
	translationsTotal   metrics.Counter
	translationDuration metrics.Histogram
	translationsRunning metrics.Gauge
}

// NewTranslatorMetricsRecorder creates a new recorder for translator metrics.
func NewTranslatorMetricsRecorder(translatorName string) TranslatorMetricsRecorder {
	if !metrics.Active() {
		return &nullTranslatorMetricsRecorder{}
	}

	m := &translatorMetrics{
		translatorName:      translatorName,
		translationsTotal:   translationsTotal,
		translationDuration: translationDuration,
		translationsRunning: translationsRunning,
	}

	return m
}

// TranslationStart is called at the start of a translation function to begin metrics
// collection and returns a function called at the end to complete metrics recording.
func (m *translatorMetrics) TranslationStart() func(error) {
	start := time.Now()

	m.translationsRunning.Add(1,
		metrics.Label{Name: translatorNameLabel, Value: m.translatorName})

	return func(err error) {
		duration := time.Since(start)

		m.translationDuration.Observe(duration.Seconds(),
			metrics.Label{Name: translatorNameLabel, Value: m.translatorName})

		result := "success"
		if err != nil {
			result = "error"
		}

		m.translationsTotal.Inc([]metrics.Label{
			{Name: translatorNameLabel, Value: m.translatorName},
			{Name: "result", Value: result},
		}...)

		m.translationsRunning.Sub(1,
			metrics.Label{Name: translatorNameLabel, Value: m.translatorName})
	}
}

var _ TranslatorMetricsRecorder = &translatorMetrics{}

type nullTranslatorMetricsRecorder struct{}

func (m *nullTranslatorMetricsRecorder) TranslationStart() func(error) {
	return func(err error) {}
}

// ResourceSyncStartTime represents the start time of a resource sync.
type ResourceSyncStartTime struct {
	Time         time.Time
	ResourceType string
	ResourceName string
	Namespace    string
	Gateway      string
}

// resourceSyncStartTimes tracks the start times of resource syncs.
type resourceSyncStartTimes struct {
	sync.RWMutex
	times map[string]map[string]map[string]map[string]ResourceSyncStartTime
}

var startTimes = &resourceSyncStartTimes{}

// ResourceSyncDetails holds the details of a resource sync operation.
type ResourceSyncDetails struct {
	Namespace    string
	Gateway      string
	ResourceType string
	ResourceName string
}

type syncStartInfo struct {
	endTime           time.Time
	details           ResourceSyncDetails
	xdsSnapshot       bool
	totalCounter      metrics.Counter
	durationHistogram metrics.Histogram
}

// Buffered channel to handle resource sync metrics updates.
// The buffer size is assumed to be sufficient for any reasonable load.
// But, this may need to be configurable in the future, if needed for very high load.
var syncCh = make(chan *syncStartInfo, 1024)

// StartResourceSyncMetricsProcessing starts a goroutine that processes resource sync metrics.
func StartResourceSyncMetricsProcessing(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case syncInfo, ok := <-syncCh:
				if !ok || syncInfo == nil {
					return
				}

				endResourceSync(syncInfo)
			}
		}
	}()
}

// IncXDSSnapshotSync records an XDS snapshot sync for a given gateway.
func IncXDSSnapshotSync(gateway, namespace string) {
	if !metrics.Active() {
		return
	}

	xdsSnapshotSyncsTotal.Inc([]metrics.Label{
		{Name: "gateway", Value: gateway},
		{Name: "namespace", Value: namespace},
	}...)
}

// StartResourceSync records the start time of a sync for a given resource and
// increments the resource syncs started counter.
func StartResourceSync(resourceName string, labels ResourceMetricLabels) {
	if !metrics.Active() {
		return
	}

	if startResourceSync(ResourceSyncDetails{
		Namespace:    labels.Namespace,
		Gateway:      labels.Gateway,
		ResourceType: labels.Resource,
		ResourceName: resourceName,
	}) {
		resourcesSyncsStartedTotal.Inc(labels.toMetricsLabels()...)
	}
}

func startResourceSync(details ResourceSyncDetails) bool {
	startTimes.Lock()
	defer startTimes.Unlock()

	if startTimes.times == nil {
		startTimes.times = make(map[string]map[string]map[string]map[string]ResourceSyncStartTime)
	}

	if startTimes.times[details.Gateway] == nil {
		startTimes.times[details.Gateway] = make(map[string]map[string]map[string]ResourceSyncStartTime)
	}

	if startTimes.times[details.Gateway][details.ResourceType] == nil {
		startTimes.times[details.Gateway][details.ResourceType] = make(map[string]map[string]ResourceSyncStartTime)
	}

	if startTimes.times[details.Gateway][details.ResourceType][details.Namespace] == nil {
		startTimes.times[details.Gateway][details.ResourceType][details.Namespace] = make(map[string]ResourceSyncStartTime)
	}

	st := ResourceSyncStartTime{
		Time:         time.Now(),
		ResourceType: details.ResourceType,
		ResourceName: details.ResourceName,
		Namespace:    details.Namespace,
		Gateway:      details.Gateway,
	}

	curStartTime, exists := startTimes.times[details.Gateway][details.ResourceType][details.Namespace][details.ResourceName]
	if !exists {
		startTimes.times[details.Gateway][details.ResourceType][details.Namespace][details.ResourceName] = st
		curStartTime = st
	}

	if startTimes.times[details.Gateway]["XDSSnapshot"] == nil {
		startTimes.times[details.Gateway]["XDSSnapshot"] = make(map[string]map[string]ResourceSyncStartTime)
	}

	if startTimes.times[details.Gateway]["XDSSnapshot"][details.Namespace] == nil {
		startTimes.times[details.Gateway]["XDSSnapshot"][details.Namespace] = make(map[string]ResourceSyncStartTime)
	}

	startTimes.times[details.Gateway]["XDSSnapshot"][details.Namespace][details.ResourceName] = curStartTime

	return !exists
}

// EndResourceSync records the end time of a sync for a given resource and
// updates the resource sync metrics accordingly.
// Returns true if the sync was added to the channel, false if the channel is full.
// If the channel is full, an error is logged to call attention to the issue. The caller is not expected to handle this case.
func EndResourceSync(
	details ResourceSyncDetails,
	isXDSSnapshot bool,
	totalCounter metrics.Counter,
	durationHistogram metrics.Histogram,
) bool {
	if !metrics.Active() {
		return true
	}

	// Add syncStartInfo to the channel for metrics processing.
	// If the channel is full, something is probably wrong, but translations shouldn't stop because of a metrics processing issue.
	// In that case, updating the metrics will be dropped, and translations will continue processing.
	// This will cause the metrics to become invalid, so an error is logged to call attention to the issue.
	select {
	case syncCh <- &syncStartInfo{
		endTime:           time.Now(),
		details:           details,
		xdsSnapshot:       isXDSSnapshot,
		totalCounter:      totalCounter,
		durationHistogram: durationHistogram,
	}:
		return true
	default:
		logger.Log(context.Background(), slog.LevelError,
			"resource metrics sync channel is full, dropping end sync metrics update",
			"gateway", details.Gateway,
			"namespace", details.Namespace,
			"resourceType", details.ResourceType,
			"resourceName", details.ResourceName,
			"xdsSnapshot", isXDSSnapshot,
		)
		resourcesUpdatesDroppedTotal.Inc()
		return false
	}
}

func endResourceSync(syncInfo *syncStartInfo) {
	startTimes.Lock()
	defer startTimes.Unlock()

	if startTimes.times == nil {
		return
	}

	if startTimes.times[syncInfo.details.Gateway] == nil {
		return
	}

	rn := syncInfo.details.ResourceName
	rt := syncInfo.details.ResourceType

	if syncInfo.xdsSnapshot {
		rt = "XDSSnapshot"
		resourceTypeStartTimes, exists := startTimes.times[syncInfo.details.Gateway][rt]
		if !exists {
			return
		}

		deleteResources := map[string]map[string]struct{}{}

		for _, namespaceStartTimes := range resourceTypeStartTimes {
			for resourceName, st := range namespaceStartTimes {
				syncInfo.totalCounter.Inc([]metrics.Label{
					{Name: "gateway", Value: st.Gateway},
					{Name: "namespace", Value: st.Namespace},
					{Name: "resource", Value: st.ResourceType},
				}...)

				syncInfo.durationHistogram.Observe(syncInfo.endTime.Sub(st.Time).Seconds(), []metrics.Label{
					{Name: "gateway", Value: st.Gateway},
					{Name: "namespace", Value: st.Namespace},
					{Name: "resource", Value: st.ResourceType},
				}...)

				if deleteResources[st.Namespace] == nil {
					deleteResources[st.Namespace] = map[string]struct{}{}
				}

				deleteResources[st.Namespace][resourceName] = struct{}{}
			}
		}

		for namespace, resources := range deleteResources {
			for resourceName := range resources {
				delete(startTimes.times[syncInfo.details.Gateway][rt][namespace], resourceName)

				if len(startTimes.times[syncInfo.details.Gateway][rt][namespace]) == 0 {
					delete(startTimes.times[syncInfo.details.Gateway][rt], namespace)
				}

				if len(startTimes.times[syncInfo.details.Gateway][rt]) == 0 {
					delete(startTimes.times[syncInfo.details.Gateway], rt)
				}

				if len(startTimes.times[syncInfo.details.Gateway]) == 0 {
					delete(startTimes.times, syncInfo.details.Gateway)
				}
			}
		}

		return
	}

	if startTimes.times[syncInfo.details.Gateway][rt] == nil {
		return
	}

	if startTimes.times[syncInfo.details.Gateway][rt][syncInfo.details.Namespace] == nil {
		return
	}

	st, exists := startTimes.times[syncInfo.details.Gateway][rt][syncInfo.details.Namespace][rn]
	if !exists {
		return
	}

	syncInfo.totalCounter.Inc([]metrics.Label{
		{Name: "gateway", Value: st.Gateway},
		{Name: "namespace", Value: st.Namespace},
		{Name: "resource", Value: st.ResourceType},
	}...)

	syncInfo.durationHistogram.Observe(syncInfo.endTime.Sub(st.Time).Seconds(), []metrics.Label{
		{Name: "gateway", Value: st.Gateway},
		{Name: "namespace", Value: st.Namespace},
		{Name: "resource", Value: st.ResourceType},
	}...)

	delete(startTimes.times[syncInfo.details.Gateway][rt][syncInfo.details.Namespace], rn)

	if len(startTimes.times[syncInfo.details.Gateway][rt][syncInfo.details.Namespace]) == 0 {
		delete(startTimes.times[syncInfo.details.Gateway][rt], syncInfo.details.Namespace)
	}

	if len(startTimes.times[syncInfo.details.Gateway][rt]) == 0 {
		delete(startTimes.times[syncInfo.details.Gateway], rt)
	}

	if len(startTimes.times[syncInfo.details.Gateway]) == 0 {
		delete(startTimes.times, syncInfo.details.Gateway)
	}
}

// ResetMetrics resets the metrics from this package.
// This is provided for testing purposes only.
func ResetMetrics() {
	translationsTotal.Reset()
	translationDuration.Reset()
	translationsRunning.Reset()
	xdsSnapshotSyncsTotal.Reset()
	resourcesSyncsStartedTotal.Reset()
	resourcesUpdatesDroppedTotal.Reset()

	startTimes.Lock()
	defer startTimes.Unlock()
	startTimes.times = make(map[string]map[string]map[string]map[string]ResourceSyncStartTime)
}
