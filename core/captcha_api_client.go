package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	api2captcha "github.com/2captcha/2captcha-go"
)

// twoCaptchaAPIClient talks to the 2captcha HTTP API directly instead of going
// through api2captcha's Solve.
//
// The library is unusable for a proxied deployment for two reasons:
//
//   - Its non-upload path calls http.Get and http.PostForm, i.e. Go's
//     DefaultClient. It sets c.httpClient.Timeout and then never uses that
//     client, so a caller cannot give the API call a proxy, a timeout or a
//     context.
//   - Every transport error collapses to a bare ErrNetwork with the cause
//     discarded, so "api2captcha: Network failure" was the only evidence of
//     why solving never worked — no DNS error, no status code, nothing.
//
// Routing the API call through the same proxy the solve is bought for also
// keeps 2captcha from seeing a datacenter IP for a residential lane's work.
type twoCaptchaAPIClient struct {
	apiKey       string
	baseURL      string
	timeout      time.Duration
	pollInterval time.Duration

	mu      sync.Mutex
	clients map[string]*http.Client
}

const (
	twoCaptchaBaseURL          = "https://2captcha.com"
	twoCaptchaPollInterval     = 5 * time.Second
	twoCaptchaRequestTimeout   = 30 * time.Second
	twoCaptchaNotReadyResponse = "CAPCHA_NOT_READY"
)

func newTwoCaptchaAPIClient(apiKey string, timeout time.Duration) *twoCaptchaAPIClient {
	return &twoCaptchaAPIClient{
		apiKey:       apiKey,
		baseURL:      twoCaptchaBaseURL,
		timeout:      timeout,
		pollInterval: twoCaptchaPollInterval,
		clients:      map[string]*http.Client{},
	}
}

// httpClientFor returns a client egressing through proxyURL, or direct when it
// is empty. Clients are cached per proxy so a solve does not rebuild a TLS
// stack it could reuse.
func (c *twoCaptchaAPIClient) httpClientFor(proxyURL string) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)

	c.mu.Lock()
	defer c.mu.Unlock()
	if client, ok := c.clients[proxyURL]; ok {
		return client, nil
	}

	transport := &http.Transport{}
	if proxyURL != "" {
		normalized, err := NormalizeProxyURL(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("captcha api proxy: %w", err)
		}
		parsed, err := url.Parse(normalized)
		if err != nil {
			return nil, fmt.Errorf("captcha api proxy: %w", err)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}

	client := &http.Client{Transport: transport, Timeout: twoCaptchaRequestTimeout}
	c.clients[proxyURL] = client
	return client, nil
}

type twoCaptchaResponse struct {
	Status    int    `json:"status"`
	Request   string `json:"request"`
	ErrorText string `json:"error_text"`
}

// Solve submits the captcha and polls for its answer, returning the token and
// the 2captcha job id. transportProxyURL routes the API call itself; the proxy
// the worker should solve through is carried in req.Params by the caller.
func (c *twoCaptchaAPIClient) Solve(req api2captcha.Request, transportProxyURL string) (string, string, error) {
	client, err := c.httpClientFor(transportProxyURL)
	if err != nil {
		return "", "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	id, err := c.submit(ctx, client, req)
	if err != nil {
		return "", "", err
	}

	token, err := c.awaitResult(ctx, client, id)
	if err != nil {
		return "", id, err
	}
	return token, id, nil
}

func (c *twoCaptchaAPIClient) submit(ctx context.Context, client *http.Client, req api2captcha.Request) (string, error) {
	values := url.Values{"key": {c.apiKey}, "json": {"1"}}
	for key, value := range req.Params {
		values.Set(key, value)
	}

	body, err := c.post(ctx, client, c.baseURL+"/in.php", values)
	if err != nil {
		return "", fmt.Errorf("submit captcha: %w", err)
	}
	if body.Status != 1 {
		return "", fmt.Errorf("submit captcha rejected: %s", describeTwoCaptchaError(body))
	}
	return body.Request, nil
}

func (c *twoCaptchaAPIClient) awaitResult(ctx context.Context, client *http.Client, id string) (string, error) {
	query := url.Values{"key": {c.apiKey}, "action": {"get"}, "id": {id}, "json": {"1"}}
	endpoint := c.baseURL + "/res.php?" + query.Encode()

	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return "", fmt.Errorf("captcha not solved within %s (last error: %w)", c.timeout, lastErr)
			}
			return "", fmt.Errorf("captcha not solved within %s", c.timeout)
		case <-ticker.C:
		}

		body, err := c.get(ctx, client, endpoint)
		if err != nil {
			// A single failed poll is not fatal; the job is still queued.
			lastErr = err
			continue
		}
		if body.Status == 1 {
			return body.Request, nil
		}
		if body.Request == twoCaptchaNotReadyResponse {
			continue
		}
		return "", fmt.Errorf("captcha unsolved: %s", describeTwoCaptchaError(body))
	}
}

func (c *twoCaptchaAPIClient) post(ctx context.Context, client *http.Client, endpoint string, values url.Values) (twoCaptchaResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return twoCaptchaResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(client, request)
}

func (c *twoCaptchaAPIClient) get(ctx context.Context, client *http.Client, endpoint string) (twoCaptchaResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return twoCaptchaResponse{}, err
	}
	return c.do(client, request)
}

func (c *twoCaptchaAPIClient) do(client *http.Client, request *http.Request) (twoCaptchaResponse, error) {
	response, err := client.Do(request)
	if err != nil {
		// Keep the cause. This is the whole reason for not using the library.
		return twoCaptchaResponse{}, err
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return twoCaptchaResponse{}, err
	}
	if response.StatusCode != http.StatusOK {
		return twoCaptchaResponse{}, fmt.Errorf("2captcha returned HTTP %d: %s",
			response.StatusCode, strings.TrimSpace(string(raw)))
	}

	var body twoCaptchaResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		return twoCaptchaResponse{}, fmt.Errorf("2captcha returned unparsable body %q: %w",
			strings.TrimSpace(string(raw)), err)
	}
	return body, nil
}

func describeTwoCaptchaError(body twoCaptchaResponse) string {
	if body.ErrorText != "" {
		return fmt.Sprintf("%s (%s)", body.Request, body.ErrorText)
	}
	if body.Request != "" {
		return body.Request
	}
	return "unknown error"
}
