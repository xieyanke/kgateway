# Working With Metrics

## Metrics Package
The [metrics](/pkg/metrics/metrics.go) package provides constructors to create metric recorders:
* `NewCounter(opts CounterOpts, labels []string) Counter`
* `NewHistogram(opts HistogramOpts, labels []string) Histogram`
* `NewGauge(opts GaugeOpts, labels []string) Gauge`

These constructors handle registering the metrics with a metrics registry. By default, a new empty registry is created at startup. This can be replaced with a custom registry, or a built-in registry used by controller-runtime can be enabled by using:
* `SetRegistry(useBuiltinRegistry bool, r RegistererGatherer)`

The underlying implementation is based on [github.com/prometheus/client_golang/prometheus](github.com/prometheus/client_golang/prometheus).

### Best practices and common patterns
* Metrics are expected to have a namespace and subsystem defined in their options
  * The default namespace of "kgateway" will be used if no namespace is provided. This will likely be the correct namespace.
* When passing labels to methods such as `Add(...)` or `Set(...)`, consider creating a struct to hold the label values with a method to convert it into a slice of Labels. This improves readability and ensures that any missed labels are present with a default ("") value.
  * See [`resourceMetricLabels`](/internal/kgateway/krtcollections/metrics.go) for an example.
* Follow the [Prometheus Metric and Label Naming Guide](https://prometheus.io/docs/practices/naming/) when possible.
  * `promlinter` is now used in static code analysis to validate metric names, types, and metadata.
* The metrics package supports an `Active() bool` method with the underlying value evaluated at startup, and can not be meaningfully changed during execution.
  * In a test context, the value defaults to `true` and can be set with `metrics.SetActive(bool)`

## Metric collection packages
Several packages have interfaces created to standardize collection of metrics around existing frameworks
* [controllerMetricsRecorder](/internal/kgateway/controller/metrics.go) for [/internal/kgateway/controller](/internal/kgateway/controller/)
  * Created by `newControllerMetricsRecorder(controllerName string) controllerMetricsRecorder `
* [TranslatorMetricsRecorder](/internal/kgateway/translator/metrics/metrics.go) for [/internal/kgateway/translator/](/internal/kgateway/translator/)
  * Created by `NewTranslatorMetricsRecorder(translatorName string) TranslatorMetricsRecorder`
* [StartResourceSync and EndResourceSync](/internal/kgateway/translator/metrics/metrics.go) are used to track metrics related to resource sync.
  * Used with `StartResourceSync(resourceName string, labels ResourceMetricLabels)` and `EndResourceSync(details ResourceSyncDetails, isXDSSnapshot bool, totalCounter metrics.Counter durationHistogram metrics.Histogram)`
  * `StartResourceSyncMetricsProcessing(ctx context.Context)` must be called at process startup to handle processing of resource metrics. 
* [statusSyncMetricsRecorder](/internal/kgateway/proxy_syncer/metrics.go) for the status syncer in [/internal/kgateway/proxy_syncer/](/internal/kgateway/proxy_syncer/)
  * Created by `NewStatusSyncMetricsRecorder(syncerName string) statusSyncMetricsRecorder`

Objects returned from these constructors will be unique, but the underlying metrics will be shared.

These objects all support a `Start` method, that can be placed at the beginning of processing an event:
```go
	var rErr error
	metricsRecorder := NewTranslatorMetricsRecorder("TranslateGateway")
	finishFunc := metricsRecorder.TranslationStart()
	defer func() {
		finishFunc(rErr)
	}()

	rErr := DoSomeWork()
```

These `Start` methods return a function to be defered to run on completion of the event handling, allowing collection of timing and other metrics. If the `Start` method is not called, those metrics will not be collected, but there will be no failures.


### Gathering metrics from a KRT collection
* The [metrics](/pkg/metrics/metrics.go) package provides a function used to create metrics related to KRT collection events:
  * `RegisterEvents[T any](c krt.Collection[T], f func(o krt.Event[T])) krt.Syncer`
* Event handlers can be registered for KRT collections for metrics that need to be updated on Add, Delete, and/or Update. `RegisterEvents` is a helper function that will register the passed function as an event handler of the collection. This code will run when a
KRT collection is modified.
* Example:
```go
	tcproutes := krt.WrapClient(kclient.NewDelayedInformer[*gwv1a2.TCPRoute](istioClient, gvr.TCPRoute, kubetypes.StandardInformer, filter), krtopts.ToOptions("TCPRoute")...)
	metrics.RegisterEvents(tcproutes, func(o krt.Event[*gwv1a2.TCPRoute]) {
		MyEventHandler(o)
	})
```
* This is used along with a helper function to instrument several KRT collections during [setup](internal/kgateway/krtcollections/setup.go):
```go
metrics.RegisterEvents(httpRoutes, GetResourceMetricEventHandler[*gwv1.HTTPRoute]())
```

### `kgateway_resources_updates_dropped_total` Metric
* This metric is never emitted under normal operating circumstances.
* It counts the number of times the background processing of resources metrics had to drop an update because the channel buffer was full.
* This indicates the metrics system, and probably the gateway, is overloaded.
* Once updates have been dropped, `kgateway_resources_*` metrics are no longer valid, until the process has been restarted.
* Metrics subsystems other than `kgateway_resources_` are not affected.
