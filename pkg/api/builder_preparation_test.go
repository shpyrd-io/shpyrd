package api

import (
	shpyrdv1 "github.com/shpyrd-io/shpyrd/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestProjectDetailMarksBuilderPreparationTransient(t *testing.T) {
	for _, tc := range []struct {
		name, phase, reason string
		status              metav1.ConditionStatus
		generation          int64
		transient           bool
	}{
		{"preparing", "Building", "PreparingBuilder", metav1.ConditionUnknown, 2, true},
		{"normal build", "Building", "Building", metav1.ConditionUnknown, 2, false},
		{"failed", "Failed", "PreparingBuilder", metav1.ConditionUnknown, 2, false},
		{"finished", "Running", "BuildSucceeded", metav1.ConditionTrue, 2, false},
		{"stale condition", "Building", "PreparingBuilder", metav1.ConditionUnknown, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &shpyrdv1.App{ObjectMeta: metav1.ObjectMeta{Generation: 2}, Status: shpyrdv1.AppStatus{Phase: tc.phase, Conditions: []metav1.Condition{{Type: shpyrdv1.ConditionBuilt, Status: tc.status, Reason: tc.reason, ObservedGeneration: tc.generation}}}}
			if got := detail(app, nil).Status.Transient; got != tc.transient {
				t.Fatalf("transient=%v, want %v", got, tc.transient)
			}
		})
	}
}
