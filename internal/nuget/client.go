package nuget

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// defaultFlatContainerBase is nuget.org's well-known flat-container
// endpoint. A real NuGet client discovers this (and everything else) by
// querying https://api.nuget.org/v3/index.json first; vmnet hardcodes it
// instead — a deliberate v1 simplification (documented in
// docs/en/ROADMAP.md) that trades private-feed/mirror support for a much
// smaller client.
const defaultFlatContainerBase = "https://api.nuget.org/v3-flatcontainer"

// Client talks to a NuGet v3 flat-container feed.
type Client struct {
	HTTP    *http.Client
	BaseURL string // override for tests or a private/mirror feed
}

func NewClient() *Client {
	return &Client{HTTP: http.DefaultClient}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimSuffix(c.BaseURL, "/")
	}
	return defaultFlatContainerBase
}

// ListVersions returns every published version of id. The flat-container
// index already returns them ascending, oldest first.
func (c *Client) ListVersions(id string) ([]string, error) {
	url := fmt.Sprintf("%s/%s/index.json", c.base(), strings.ToLower(id))
	resp, err := c.httpClient().Get(url)
	if err != nil {
		return nil, fmt.Errorf("nuget: listing versions for %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("nuget: package %q not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nuget: listing versions for %s: HTTP %d", id, resp.StatusCode)
	}

	var body struct {
		Versions []string `json:"versions"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("nuget: decoding version list for %s: %w", id, err)
	}
	return body.Versions, nil
}

// LatestVersion returns id's highest published version.
func (c *Client) LatestVersion(id string) (string, error) {
	versions, err := c.ListVersions(id)
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("nuget: %s has no published versions", id)
	}
	return versions[len(versions)-1], nil
}

// Download fetches the raw .nupkg bytes for id@version.
func (c *Client) Download(id, version string) ([]byte, error) {
	idl, vl := strings.ToLower(id), strings.ToLower(version)
	url := fmt.Sprintf("%s/%s/%s/%s.%s.nupkg", c.base(), idl, vl, idl, vl)

	resp, err := c.httpClient().Get(url)
	if err != nil {
		return nil, fmt.Errorf("nuget: downloading %s@%s: %w", id, version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("nuget: %s@%s not found", id, version)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nuget: downloading %s@%s: HTTP %d", id, version, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, fmt.Errorf("nuget: reading %s@%s: %w", id, version, err)
	}
	return data, nil
}
