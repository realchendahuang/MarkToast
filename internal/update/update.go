// Package update checks the latest GitHub release against the running version.
// It deliberately does not install anything; packaging and signing stay in the
// release scripts.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Result describes one release check.
type Result struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Newer   bool   `json:"newer"`
	URL     string `json:"url,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

// Check queries GitHub Releases for owner/repo. baseURL may be empty for
// api.github.com, or a test server.
func Check(current, repository, token, baseURL string) (Result, error) {
	result := Result{Current: current}
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", base, repository)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return result, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "MarkToast/"+current)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return result, fmt.Errorf("no releases found (private repos need GITHUB_TOKEN)")
	}
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("GitHub API returned %s", response.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return result, err
	}
	result.Latest = strings.TrimPrefix(release.TagName, "v")
	result.URL = release.HTMLURL
	result.Notes = release.Body
	result.Newer = compare(result.Latest, strings.TrimPrefix(current, "v")) > 0
	return result, nil
}

// compare compares dotted numeric versions, returning -1, 0 or 1.
func compare(a, b string) int {
	ap := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bp := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(ap) || i < len(bp); i++ {
		var av, bv int
		if i < len(ap) {
			av, _ = strconv.Atoi(strings.SplitN(ap[i], "-", 2)[0])
		}
		if i < len(bp) {
			bv, _ = strconv.Atoi(strings.SplitN(bp[i], "-", 2)[0])
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}
