package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/tidwall/gjson"
)

// DoGrokNativeResponsesJSON is the Gin-free native Grok search request path.
func (s *GatewayService) DoGrokNativeResponsesJSON(ctx context.Context, account *Account, body []byte) ([]byte, error) {
	if s == nil || s.httpUpstream == nil || account == nil {
		return nil, errors.New("grok search is not configured")
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	target, err := buildGrokResponsesURL(account, s.cfg, s.settingService)
	if err != nil {
		return nil, err
	}
	if gjson.ValidBytes(body) && gjson.GetBytes(body, "model").String() == "" {
		// Keep the request model explicit for providers that require it.
		body = []byte(`{"model":"grok-4.6"}`)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultGrokUpstreamUserAgent())
	applyGrokCLIHeaders(req.Header)
	account.ApplyHeaderOverrides(req.Header)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	result, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: result}
	}
	return result, nil
}
