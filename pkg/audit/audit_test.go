package audit

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// An entry round-trips through the Event's annotations, the new fields
// included, and the sink hears it after the write.
func TestRecordListAndSink(t *testing.T) {
	k := fake.NewSimpleClientset()
	ref := AppRefIn("p-abc", "abc")
	var heard []Entry
	Sink = func(r Ref, e Entry) {
		if r != ref {
			t.Errorf("sink ref = %+v, want %+v", r, ref)
		}
		heard = append(heard, e)
	}
	t.Cleanup(func() { Sink = nil })

	in := Entry{Actor: "ana@acme.test (token laptop)", Action: "release.succeeded", Target: "v1", Detail: "Initial deploy",
		Via: "controller", Realm: "workspace", Subject: "01J9…", Client: "cli"}
	if err := Record(context.Background(), k, ref, in); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 1 || heard[0].Action != "release.succeeded" || heard[0].Subject != in.Subject || heard[0].Client != "cli" {
		t.Fatalf("sink heard %+v", heard)
	}

	// The fake clientset ignores field selectors: list by hand, as List
	// would, from what was written.
	evs, err := k.CoreV1().Events("p-abc").List(context.Background(), metav1.ListOptions{})
	if err != nil || len(evs.Items) != 1 {
		t.Fatalf("events = %v, %v", len(evs.Items), err)
	}
	a := evs.Items[0].Annotations
	if a[AnnotationSubject] != in.Subject || a[AnnotationClient] != "cli" || a[AnnotationAction] != in.Action || a[AnnotationVia] != "controller" {
		t.Errorf("annotations = %v", a)
	}
	if evs.Items[0].Message != "release.succeeded v1: Initial deploy by ana@acme.test (token laptop)" {
		t.Errorf("message = %q", evs.Items[0].Message)
	}
}

// Without a client nothing is written and the sink stays quiet: tests
// and the CLI's dry paths call Record with nil.
func TestRecordWithoutClientIsSilent(t *testing.T) {
	called := false
	Sink = func(Ref, Entry) { called = true }
	t.Cleanup(func() { Sink = nil })
	if err := Record(context.Background(), nil, ClusterRef("shpyrd-system"), Entry{Action: "x"}); err != nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
