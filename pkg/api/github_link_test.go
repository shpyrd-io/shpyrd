package api

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/tokenseal"
)

func TestTheServerMakesTheTokenKey(t *testing.T) {
	s, _, _ := githubServer(t)
	if s.githubBox == nil {
		t.Fatal("no token key")
	}
	sec, err := s.kube.Kube.CoreV1().Secrets("shpyrd-system").Get(context.Background(), tokenseal.SecretName, metav1.GetOptions{})
	if err != nil || len(sec.Data[tokenseal.KeyName]) != 32 {
		t.Fatalf("secret: err %v, key length %d, want 32", err, len(sec.Data[tokenseal.KeyName]))
	}
}
