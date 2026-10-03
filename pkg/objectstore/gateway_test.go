package objectstore

import (
	"strings"
	"testing"
)

func TestLogicalBucketsHaveUnambiguousBoundedNames(t *testing.T) {
	a, b := LogicalBucketName("app-foo-bar", "backups"), LogicalBucketName("app-foo", "bar-backups")
	if a == b {
		t.Fatal("distinct consumers share storage")
	}
	if a != LogicalBucketName("app-foo-bar", "backups") {
		t.Fatal("unstable bucket")
	}
	if name := LogicalBucketName(strings.Repeat("n", 63), strings.Repeat("b", 63)); len(name) > 63 {
		t.Fatalf("S3 name too long: %d", len(name))
	}
}
