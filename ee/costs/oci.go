//go:build !foss

package costs

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shpyrd-io/shpyrd/pkg/store"
)

// The provider's bill, from OCI: the Usage API's cost per resource, service
// and SKU for each day, and each resource's tags from the Search API (a
// usage query groups by one tag at a time, and the same cost repeats under
// each). Requests are signed the way OCI asks (draft-cavage HTTP
// signatures, rsa-sha256), with an API key of a user the operator gives in
// the Secret OCISecretName. Read-only: it never changes anything at OCI.

// OCISecretName holds the OCI API key: tenancy, user, fingerprint, region
// and key (PEM).
const OCISecretName = "shpyrd-costs-oci"

var errNoCredentials = errors.New("no OCI credentials")

// ociCredentials are an API key's parts.
type ociCredentials struct {
	Tenancy, User, Fingerprint, Region string
	Key                                *rsa.PrivateKey
}

// ociEndpoints point the driver elsewhere (tests); empty is OCI's.
type ociEndpoints struct {
	usage, search string
}

func parseRSAKey(raw []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("the key is not PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the key is not RSA")
	}
	return rk, nil
}

func (c *Collector) ociCredentials(ctx context.Context) (*ociCredentials, error) {
	if c.Kube == nil {
		return nil, errNoCredentials
	}
	sec, err := c.Kube.CoreV1().Secrets(c.Namespace).Get(ctx, OCISecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, errNoCredentials
	}
	if err != nil {
		return nil, err
	}
	get := func(k string) string { return strings.TrimSpace(string(sec.Data[k])) }
	key, err := parseRSAKey(sec.Data["key"])
	if err != nil {
		return nil, err
	}
	cr := &ociCredentials{Tenancy: get("tenancy"), User: get("user"), Fingerprint: get("fingerprint"), Region: get("region"), Key: key}
	if cr.Tenancy == "" || cr.User == "" || cr.Fingerprint == "" || cr.Region == "" {
		return nil, errors.New("the OCI credentials lack tenancy, user, fingerprint or region")
	}
	return cr, nil
}

// sign adds OCI's signature to a request whose body is given.
func (cr *ociCredentials) sign(req *http.Request, body []byte) error {
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	headers := []string{"date", "(request-target)", "host"}
	if req.Method == http.MethodPost || req.Method == http.MethodPut {
		sum := sha256.Sum256(body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Length", fmt.Sprint(len(body)))
		req.Header.Set("X-Content-Sha256", base64.StdEncoding.EncodeToString(sum[:]))
		headers = append(headers, "content-length", "content-type", "x-content-sha256")
	}
	var lines []string
	for _, h := range headers {
		switch h {
		case "(request-target)":
			lines = append(lines, "(request-target): "+strings.ToLower(req.Method)+" "+req.URL.RequestURI())
		case "host":
			lines = append(lines, "host: "+req.URL.Host)
		default:
			lines = append(lines, h+": "+req.Header.Get(h))
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	sig, err := rsa.SignPKCS1v15(rand.Reader, cr.Key, crypto.SHA256, digest[:])
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf(`Signature version="1",keyId="%s/%s/%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		cr.Tenancy, cr.User, cr.Fingerprint, strings.Join(headers, " "), base64.StdEncoding.EncodeToString(sig)))
	return nil
}

// post sends a signed POST and decodes the answer; it returns OCI's next
// page, "" at the last.
func (cr *ociCredentials) post(ctx context.Context, client *http.Client, endpoint, page string, payload, into any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if page != "" {
		q := u.Query()
		q.Set("page", page)
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	if err := cr.sign(req, body); err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return "", fmt.Errorf("oci %s: %s: %s", u.Host, res.Status, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		return "", err
	}
	return res.Header.Get("opc-next-page"), nil
}

// usageItem is one item of the Usage API's summarized usages.
type usageItem struct {
	ResourceID       string    `json:"resourceId"`
	Service          string    `json:"service"`
	SKUName          string    `json:"skuName"`
	Unit             string    `json:"unit"`
	ComputedAmount   *float64  `json:"computedAmount"`
	ComputedQuantity *float64  `json:"computedQuantity"`
	Currency         string    `json:"currency"`
	Start            time.Time `json:"timeUsageStarted"`
	End              time.Time `json:"timeUsageEnded"`
}

// searchItem is one resource the Search API answers.
type searchItem struct {
	Identifier   string                    `json:"identifier"`
	ResourceType string                    `json:"resourceType"`
	FreeformTags map[string]string         `json:"freeformTags"`
	DefinedTags  map[string]map[string]any `json:"definedTags"`
	SystemTags   map[string]map[string]any `json:"systemTags"`
}

func (s searchItem) tags() map[string]string {
	out := map[string]string{}
	for k, v := range s.FreeformTags {
		out[k] = v
	}
	for _, group := range []map[string]map[string]any{s.DefinedTags, s.SystemTags} {
		for ns, kv := range group {
			for k, v := range kv {
				out[ns+"."+k] = fmt.Sprint(v)
			}
		}
	}
	return out
}

// collectBill reads the bill of the days before the given one.
func (c *Collector) collectBill(ctx context.Context, day time.Time) error {
	cr, err := c.ociCredentials(ctx)
	if err != nil {
		return err
	}
	days := c.DaysBack
	if days <= 0 {
		days = 3
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	usageURL := firstNonEmpty(c.ociEndpoints.usage, "https://usageapi."+cr.Region+".oci.oraclecloud.com/20200107/usage")
	searchURL := firstNonEmpty(c.ociEndpoints.search, "https://query."+cr.Region+".oci.oraclecloud.com/20180409/resources")
	from := day.Add(-time.Duration(days) * 24 * time.Hour)
	var items []usageItem
	for page := ""; ; {
		var out struct {
			Items []usageItem `json:"items"`
		}
		next, err := cr.post(ctx, client, usageURL, page, map[string]any{
			"tenantId":          cr.Tenancy,
			"timeUsageStarted":  from.Format(time.RFC3339),
			"timeUsageEnded":    day.Format(time.RFC3339),
			"granularity":       "DAILY",
			"queryType":         "COST",
			"isAggregateByTime": false,
			"groupBy":           []string{"resourceId", "service", "skuName", "unit"},
		}, &out)
		if err != nil {
			return err
		}
		items = append(items, out.Items...)
		if next == "" {
			break
		}
		page = next
	}
	tags := map[string]searchItem{}
	for page := ""; ; {
		var out struct {
			Items []searchItem `json:"items"`
		}
		next, err := cr.post(ctx, client, searchURL, page, map[string]any{"type": "Structured", "query": "query all resources", "matchingContextType": "NONE"}, &out)
		if err != nil {
			return err
		}
		for _, it := range out.Items {
			tags[it.Identifier] = it
		}
		if next == "" {
			break
		}
		page = next
	}
	_, err = c.Store.UpsertCostLines(ctx, billLines(items, tags))
	return err
}

// billLines turns the Usage API's items into real cost lines, with each
// resource's tags.
func billLines(items []usageItem, tags map[string]searchItem) []store.CostLine {
	out := make([]store.CostLine, 0, len(items))
	for _, it := range items {
		if it.ComputedAmount == nil && it.ComputedQuantity == nil {
			continue
		}
		l := store.CostLine{
			Kind: store.CostReal, Source: "oci", Start: it.Start.UTC(), End: it.End.UTC(),
			Service: it.Service, SKU: it.SKUName, Resource: it.ResourceID, ResourceType: ocidType(it.ResourceID),
			Quantity: it.ComputedQuantity, Unit: it.Unit, Cost: it.ComputedAmount, Currency: it.Currency,
		}
		if s, ok := tags[it.ResourceID]; ok {
			l.Tags = s.tags()
			if s.ResourceType != "" {
				l.ResourceType = s.ResourceType
			}
		}
		out = append(out, l)
	}
	return out
}

// ocidType is the kind of resource an OCID names: ocid1.<type>.<realm>....
func ocidType(ocid string) string {
	parts := strings.Split(ocid, ".")
	if len(parts) > 2 && parts[0] == "ocid1" {
		return parts[1]
	}
	return ""
}
