package homiris

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testConfig(baseURL string, httpClient *http.Client) Config {
	return Config{
		BaseURL:     baseURL,
		Login:       "123456",
		Password:    "secret",
		ClientToken: "client-token",
		PhoneType:   "iPhone 15",
		HTTPClient:  httpClient,
	}
}

func TestClientHomepage(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/token":
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "Basic client-token", r.Header.Get("Authorization"))
			assert.NoError(t, r.ParseForm())
			assert.Equal(t, "client_credentials", r.Form.Get("grant_type"))
			assert.Equal(t, "PRODUCTION", r.Form.Get("scope"))
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/smartphone/production/1.0.0/connect":
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
			assert.Equal(t, "MOB-ABO", r.Header.Get("Eps-Ctx-Source"))
			assert.Equal(t, "123456", r.Header.Get("Eps-Ctx-Username"))
			_, _ = w.Write([]byte(`{"idSession":"session-id"}`))
		case "/smartphone/production/1.0.0/homepage/session-id":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"systemLastState":{"securityMode":"TOTAL","gsmLevel":4}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(testConfig(server.URL, server.Client()))

	homepage, err := client.Homepage(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "TOTAL", homepage.SecurityMode())
	assert.Equal(t, StateArmedAway, homepage.AlarmState())
	assert.Equal(t, int64(3), requests.Load())
}

func TestClientReusesSession(t *testing.T) {
	var tokenRequests, connectRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenRequests.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/smartphone/production/1.0.0/connect":
			connectRequests.Add(1)
			_, _ = w.Write([]byte(`{"idSession":"session-id"}`))
		default:
			_, _ = w.Write([]byte(`{"systemLastState":{"securityMode":"ARRET"}}`))
		}
	}))
	defer server.Close()

	client := New(testConfig(server.URL, server.Client()))

	for range 3 {
		homepage, err := client.Homepage(t.Context())
		require.NoError(t, err)
		assert.Equal(t, StateDisarmed, homepage.AlarmState())
	}
	assert.Equal(t, int64(1), tokenRequests.Load())
	assert.Equal(t, int64(1), connectRequests.Load())
}

func TestClientRenewsExpiredSession(t *testing.T) {
	var connectRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/smartphone/production/1.0.0/connect":
			session := strconv.FormatInt(connectRequests.Add(1), 10)
			_, _ = w.Write([]byte(`{"idSession":"session-` + session + `"}`))
		case "/smartphone/production/1.0.0/homepage/session-1":
			http.Error(w, "session expired", http.StatusUnauthorized)
		default:
			_, _ = w.Write([]byte(`{"systemLastState":{"securityMode":"PARTIEL 2"}}`))
		}
	}))
	defer server.Close()

	client := New(testConfig(server.URL, server.Client()))

	homepage, err := client.Homepage(t.Context())
	require.NoError(t, err)
	assert.Equal(t, StateArmedNight, homepage.AlarmState())
	assert.Equal(t, int64(2), connectRequests.Load())
}

func TestClientReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid client", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(testConfig(server.URL, server.Client()))

	_, err := client.Homepage(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "get access token")
	assert.ErrorContains(t, err, "401 Unauthorized: invalid client")
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/token" {
			http.Redirect(w, r, "/replayed", http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"leaked"}`))
	}))
	defer server.Close()

	client := New(testConfig(server.URL, nil))

	_, err := client.Homepage(t.Context())
	require.Error(t, err)
	assert.Equal(t, int64(1), requests.Load())
}
