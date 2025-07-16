package kube

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
)

var _ recorder.Provider = new(noOpProvider)

func NewNoOpProvider() *noOpProvider {
	return &noOpProvider{}
}

type noOpProvider struct {
}

func (n *noOpProvider) GetEventRecorderFor(name string) record.EventRecorder {
	return &noOpEventRecorder{}
}

type noOpEventRecorder struct{}

func (n *noOpEventRecorder) Event(object runtime.Object, eventType, reason, message string) {}

func (n *noOpEventRecorder) Eventf(object runtime.Object, eventType, reason, messageFmt string, args ...interface{}) {
}

func (n *noOpEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
}
