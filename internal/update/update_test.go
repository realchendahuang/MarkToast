package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://example.test/r","body":"notes"}`))
	}))
	defer server.Close()
	result, err := Check("1.1.0", "owner/repo", "token", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result.Latest != "1.2.0" || !result.Newer || result.URL == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	older, err := Check("1.2.0", "owner/repo", "", server.URL)
	if err != nil || older.Newer {
		t.Fatalf("expected up to date: %+v %v", older, err)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.9", 1},
		{"1.2.0", "1.2.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"v2.0.0", "v1.9.9", 1},
	}
	for _, c := range cases {
		if got := compare(c.a, c.b); got != c.want {
			t.Fatalf("compare(%q,%q)=%d", c.a, c.b, got)
		}
	}
}
