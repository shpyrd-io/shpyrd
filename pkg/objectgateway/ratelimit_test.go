package objectgateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A request past its bucket's rate is answered as S3 answers it: 503,
// SlowDown in the body, Retry-After; the next bucket is not slowed by it,
// and a request without a bucket is not limited here.
func TestARequestPastTheBucketsRateIsSlowedDown(t *testing.T) {
	app := fiber.New()
	app.Use("/", newBucketLimiters(1, 2).middleware())
	app.All("/*", func(c fiber.Ctx) error { return c.SendStatus(http.StatusOK) })
	codes := []int{}
	var last *http.Response
	for i := 0; i < 4; i++ {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/busy/key", nil))
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, resp.StatusCode)
		last = resp
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 503 || codes[3] != 503 {
		t.Fatalf("codes = %v, want the burst of two then slow-downs", codes)
	}
	body, _ := io.ReadAll(last.Body)
	if !strings.Contains(string(body), "<Code>SlowDown</Code>") || last.Header.Get("Retry-After") != "1" || !strings.Contains(last.Header.Get("Content-Type"), "xml") {
		t.Errorf("the slow-down answer: %d %v %s", last.StatusCode, last.Header, body)
	}
	for _, path := range []string{"/quiet/key", "/"} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("%s = %d, want 200", path, resp.StatusCode)
		}
	}
}
