package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// apiError is the stable error envelope returned by the core.
type apiError struct {
	code   string
	status int
}

func (e *apiError) Error() string {
	return fmt.Sprintf("api error %s (%d)", e.code, e.status)
}

// networkError hides the underlying transport detail: it can contain a URL but
// never the API key, which is only sent in a header.
type networkError struct {
	err error
}

func (e *networkError) Error() string { return e.err.Error() }

// invalidResponseError means the core answered with something that is not JSON.
type invalidResponseError struct{ err error }

func (e *invalidResponseError) Error() string { return e.err.Error() }

// apiClient is the small HTTP client shared by all commands.
type apiClient struct {
	base       string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

// newAPIClient builds a client, using the system roots plus an optional CA file.
func newAPIClient(rawURL, apiKey, caFile, version string) (*apiClient, error) {
	base, err := normalizeURL(rawURL)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(caFile) != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, &caFileError{err: err}
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, &caFileError{err: errors.New("the file contains no PEM certificate")}
		}
		tlsConfig.RootCAs = roots
	}
	transport.TLSClientConfig = tlsConfig
	return &apiClient{
		base:      base,
		apiKey:    apiKey,
		userAgent: "talaiactl/" + version,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}, nil
}

// caFileError marks a CA bundle that could not be used.
type caFileError struct{ err error }

func (e *caFileError) Error() string { return e.err.Error() }

// normalizeURL validates the configured core URL and removes a trailing slash.
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errInvalidURL
	}
	return strings.TrimRight(raw, "/"), nil
}

// get performs a GET and returns the raw response body.
func (c *apiClient) get(ctx context.Context, path string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, path, nil)
}

// post performs a POST with a JSON body and returns the raw response body.
func (c *apiClient) post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.request(ctx, http.MethodPost, path, body)
}

func (c *apiClient) request(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, &networkError{err: err}
	}
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, &networkError{err: err}
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, &networkError{err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(payload, &envelope)
		code := strings.TrimSpace(envelope.Error.Code)
		if code == "" {
			code = "internal"
		}
		return nil, &apiError{code: code, status: response.StatusCode}
	}
	return payload, nil
}

// server is the part of the server list talaiactl uses.
type server struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Revoked   bool   `json:"revoked"`
	Limited   bool   `json:"limited"`
	Status    string `json:"status"`
	Live      *struct {
		AgentVersion string `json:"agent_version"`
	} `json:"live"`
}

type serversResponse struct {
	Servers []server `json:"servers"`
}

type container struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Image          string `json:"image"`
	State          string `json:"state"`
	Status         string `json:"status"`
	ComposeProject string `json:"compose_project"`
	ComposeService string `json:"compose_service"`
}

type containersResponse struct {
	Connected       bool        `json:"connected"`
	DockerAvailable bool        `json:"docker_available"`
	Containers      []container `json:"containers"`
}

type stackEntry struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Name       string `json:"name"`
	State      string `json:"state"`
}

type allStacksResponse struct {
	Stacks []stackEntry `json:"stacks"`
}

type serverStacksResponse struct {
	Connected bool     `json:"connected"`
	Stacks    []string `json:"stacks"`
	Adopted   []string `json:"adopted"`
	Broken    []string `json:"broken"`
	Adoptable []string `json:"adoptable"`
}

type job struct {
	ID         string         `json:"id"`
	ServerID   string         `json:"server_id"`
	Kind       string         `json:"kind"`
	Params     map[string]any `json:"params"`
	State      string         `json:"state"`
	Message    string         `json:"message"`
	Output     string         `json:"output"`
	CreatedBy  string         `json:"created_by"`
	CreatedAt  time.Time      `json:"created_at"`
	FinishedAt *time.Time     `json:"finished_at"`
}

type createdJob struct {
	JobID string `json:"job_id"`
}

type meResponse struct {
	User struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	} `json:"user"`
}

// getServers loads the visible servers.
func (c *apiClient) getServers(ctx context.Context) ([]server, error) {
	raw, err := c.get(ctx, "/api/v1/servers")
	if err != nil {
		return nil, err
	}
	var response serversResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, &invalidResponseError{err: err}
	}
	return response.Servers, nil
}

// getMe verifies the key and returns the signed-in user.
func (c *apiClient) getMe(ctx context.Context) (meResponse, error) {
	raw, err := c.get(ctx, "/api/v1/me")
	if err != nil {
		return meResponse{}, err
	}
	var response meResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return meResponse{}, &invalidResponseError{err: err}
	}
	return response, nil
}

// resolveServer matches an exact id first, then an exact unique name.
func (c *apiClient) resolveServer(ctx context.Context, reference string) (string, error) {
	servers, err := c.getServers(ctx)
	if err != nil {
		return "", err
	}
	for _, candidate := range servers {
		if candidate.ID == reference {
			return candidate.ID, nil
		}
	}
	var matches []string
	for _, candidate := range servers {
		if candidate.Name == reference {
			matches = append(matches, candidate.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", &serverNotFoundError{reference: reference}
	case 1:
		return matches[0], nil
	default:
		return "", &serverAmbiguousError{reference: reference, count: len(matches)}
	}
}

type serverNotFoundError struct{ reference string }

func (e *serverNotFoundError) Error() string { return "server not found: " + e.reference }

type serverAmbiguousError struct {
	reference string
	count     int
}

func (e *serverAmbiguousError) Error() string {
	return fmt.Sprintf("server name is ambiguous: %s (%d)", e.reference, e.count)
}
