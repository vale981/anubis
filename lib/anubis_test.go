package lib

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vale981/anubis"
	"github.com/vale981/anubis/data"
	"github.com/vale981/anubis/internal"
	"github.com/vale981/anubis/lib/policy"
)

func loadPolicies(t *testing.T, fname string) *policy.ParsedConfig {
	t.Helper()

	anubisPolicy, err := LoadPoliciesOrDefault(fname, anubis.DefaultDifficulty)
	if err != nil {
		t.Fatal(err)
	}

	return anubisPolicy
}

func spawnAnubis(t *testing.T, opts Options) *Server {
	t.Helper()

	s, err := New(opts)
	if err != nil {
		t.Fatalf("can't construct libanubis.Server: %v", err)
	}

	return s
}

type challenge struct {
	Challenge string `json:"challenge"`
}

func makeChallenge(t *testing.T, ts *httptest.Server) challenge {
	t.Helper()

	resp, err := ts.Client().Post(ts.URL+"/.within.website/x/cmd/anubis/api/make-challenge", "", nil)
	if err != nil {
		t.Fatalf("can't request challenge: %v", err)
	}
	defer resp.Body.Close()

	var chall challenge
	if err := json.NewDecoder(resp.Body).Decode(&chall); err != nil {
		t.Fatalf("can't read challenge response body: %v", err)
	}

	return chall
}

func TestLoadPolicies(t *testing.T) {
	for _, fname := range []string{"botPolicies.json", "botPolicies.yaml"} {
		t.Run(fname, func(t *testing.T) {
			fin, err := data.BotPolicies.Open(fname)
			if err != nil {
				t.Fatal(err)
			}
			defer fin.Close()

			if _, err := policy.ParseConfig(fin, fname, 4); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Regression test for CVE-2025-24369
func TestCVE2025_24369(t *testing.T) {
	pol := loadPolicies(t, "")
	pol.DefaultDifficulty = 4

	srv := spawnAnubis(t, Options{
		Next:   http.NewServeMux(),
		Policy: pol,

		CookieDomain:      "local.cetacean.club",
		CookiePartitioned: true,
		CookieName:        t.Name(),
	})

	ts := httptest.NewServer(internal.RemoteXRealIP(true, "tcp", srv))
	defer ts.Close()

	chall := makeChallenge(t, ts)
	calcString := fmt.Sprintf("%s%d", chall.Challenge, 0)
	calculated := internal.SHA256sum(calcString)
	nonce := 0
	elapsedTime := 420
	redir := "/"

	cli := ts.Client()
	cli.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/.within.website/x/cmd/anubis/api/pass-challenge", nil)
	if err != nil {
		t.Fatalf("can't make request: %v", err)
	}

	q := req.URL.Query()
	q.Set("response", calculated)
	q.Set("nonce", fmt.Sprint(nonce))
	q.Set("redir", redir)
	q.Set("elapsedTime", fmt.Sprint(elapsedTime))
	req.URL.RawQuery = q.Encode()

	resp, err := cli.Do(req)
	if err != nil {
		t.Fatalf("can't do challenge passing")
	}

	if resp.StatusCode == http.StatusFound {
		t.Log("Regression on CVE-2025-24369")
		t.Errorf("wanted HTTP status %d, got: %d", http.StatusForbidden, resp.StatusCode)
	}
}

func TestCookieSettings(t *testing.T) {
	pol := loadPolicies(t, "")
	pol.DefaultDifficulty = 0

	srv := spawnAnubis(t, Options{
		Next:   http.NewServeMux(),
		Policy: pol,

		CookieDomain:      "local.cetacean.club",
		CookiePartitioned: true,
		CookieName:        t.Name(),
	})

	ts := httptest.NewServer(internal.RemoteXRealIP(true, "tcp", srv))
	defer ts.Close()

	cli := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := cli.Post(ts.URL+"/.within.website/x/cmd/anubis/api/make-challenge", "", nil)
	if err != nil {
		t.Fatalf("can't request challenge: %v", err)
	}
	defer resp.Body.Close()

	var chall = struct {
		Challenge string `json:"challenge"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&chall); err != nil {
		t.Fatalf("can't read challenge response body: %v", err)
	}

	nonce := 0
	elapsedTime := 420
	redir := "/"
	calculated := ""
	calcString := fmt.Sprintf("%s%d", chall.Challenge, nonce)
	calculated = internal.SHA256sum(calcString)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/.within.website/x/cmd/anubis/api/pass-challenge", nil)
	if err != nil {
		t.Fatalf("can't make request: %v", err)
	}

	q := req.URL.Query()
	q.Set("response", calculated)
	q.Set("nonce", fmt.Sprint(nonce))
	q.Set("redir", redir)
	q.Set("elapsedTime", fmt.Sprint(elapsedTime))
	req.URL.RawQuery = q.Encode()

	resp, err = cli.Do(req)
	if err != nil {
		t.Fatalf("can't do challenge passing")
	}

	if resp.StatusCode != http.StatusFound {
		t.Errorf("wanted %d, got: %d", http.StatusFound, resp.StatusCode)
	}

	var ckie *http.Cookie
	for _, cookie := range resp.Cookies() {
		t.Logf("%#v", cookie)
		if cookie.Name == anubis.CookieName {
			ckie = cookie
			break
		}
	}
	if ckie == nil {
		t.Errorf("Cookie %q not found", anubis.CookieName)
		return
	}

	if ckie.Domain != "local.cetacean.club" {
		t.Errorf("cookie domain is wrong, wanted local.cetacean.club, got: %s", ckie.Domain)
	}

	if ckie.Partitioned != srv.opts.CookiePartitioned {
		t.Errorf("wanted partitioned flag %v, got: %v", srv.opts.CookiePartitioned, ckie.Partitioned)
	}
}

func TestCheckDefaultDifficultyMatchesPolicy(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	})

	for i := 1; i < 10; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			anubisPolicy, err := LoadPoliciesOrDefault("", i)
			if err != nil {
				t.Fatal(err)
			}

			s, err := New(Options{
				Next:           h,
				Policy:         anubisPolicy,
				ServeRobotsTXT: true,
			})
			if err != nil {
				t.Fatalf("can't construct libanubis.Server: %v", err)
			}

			req, err := http.NewRequest(http.MethodGet, "/", nil)
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Add("X-Real-Ip", "127.0.0.1")

			_, bot, err := s.check(req)
			if err != nil {
				t.Fatal(err)
			}

			if bot.Challenge.Difficulty != i {
				t.Errorf("Challenge.Difficulty is wrong, wanted %d, got: %d", i, bot.Challenge.Difficulty)
			}

			if bot.Challenge.ReportAs != i {
				t.Errorf("Challenge.ReportAs is wrong, wanted %d, got: %d", i, bot.Challenge.ReportAs)
			}
		})
	}
}
