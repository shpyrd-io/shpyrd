package controller

import (
	"strings"
	"testing"
)

// A rotation rewrites every key of CloudNativePG's app Secret that carries
// the password: the password, the URIs, the JDBC URIs and the pgpass line.
func TestRotateSecretDataTouchesEveryKey(t *testing.T) {
	data := map[string][]byte{
		"username":      []byte("app"),
		"password":      []byte("old-secret"),
		"uri":           []byte("postgresql://app:old-secret@db-rw:5432/app"),
		"fqdn-uri":      []byte("postgresql://app:old-secret@db-rw.p-shop.svc.cluster.local:5432/app?sslmode=require"),
		"jdbc-uri":      []byte("jdbc:postgresql://db-rw:5432/app?password=old-secret&user=app"),
		"fqdn-jdbc-uri": []byte("jdbc:postgresql://db-rw.p-shop.svc.cluster.local:5432/app?password=old-secret&user=app"),
		"pgpass":        []byte("db-rw:5432:app:app:old-secret\n"),
		"dbname":        []byte("app"),
	}
	rotateSecretData(data, "old-secret", "new-secret")
	for key, want := range map[string]string{
		"password":      "new-secret",
		"uri":           "postgresql://app:new-secret@db-rw:5432/app",
		"fqdn-uri":      "postgresql://app:new-secret@db-rw.p-shop.svc.cluster.local:5432/app?sslmode=require",
		"jdbc-uri":      "jdbc:postgresql://db-rw:5432/app?password=new-secret&user=app",
		"fqdn-jdbc-uri": "jdbc:postgresql://db-rw.p-shop.svc.cluster.local:5432/app?password=new-secret&user=app",
		"pgpass":        "db-rw:5432:app:app:new-secret\n",
		"dbname":        "app",
		"username":      "app",
	} {
		if got := string(data[key]); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	for key, value := range data {
		if strings.Contains(string(value), "old-secret") {
			t.Errorf("%s still carries the old password", key)
		}
	}
}
