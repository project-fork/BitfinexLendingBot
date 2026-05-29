package bitfinex

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitfinexcom/bitfinex-api-go/v2/rest"
	internalerrors "github.com/kfrico/BitfinexLendingBot/internal/errors"
)

func TestNormalizeFundingBookEntries_KeepsPositiveAskSideOnly(t *testing.T) {
	input := []*FundingBookEntry{
		{Rate: 0.0004, Amount: -500, Period: 2},
		{Rate: 0.0003, Amount: 1000, Period: 30},
		{Rate: 0.00035, Amount: 2000, Period: 30},
		{Rate: 0.0005, Amount: -1000, Period: 2},
	}

	got := normalizeFundingBookEntries(input)

	if len(got) != 2 {
		t.Fatalf("expected 2 ask-side entries, got %d", len(got))
	}
	if got[0].Amount <= 0 || got[1].Amount <= 0 {
		t.Fatalf("expected only positive ask-side entries, got %+v", got)
	}
	if got[0].Rate > got[1].Rate {
		t.Fatalf("expected ask-side entries sorted by ascending rate, got %+v", got)
	}
}

func TestNormalizeFundingBookEntries_FallsBackWhenOnlyOneSidePresent(t *testing.T) {
	input := []*FundingBookEntry{
		{Rate: 0.0005, Amount: -1000, Period: 2},
		{Rate: 0.0004, Amount: -500, Period: 2},
	}

	got := normalizeFundingBookEntries(input)

	if len(got) != 2 {
		t.Fatalf("expected fallback to preserve one-sided book, got %d entries", len(got))
	}
}

func TestGetCurrentFundingRate_ClassifiesHTTP429AsRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`["error",10100,"ratelimit: error"]`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL + "/",
	}

	_, err := client.GetFundingCandles("fUSD", "5m", 12)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), internalerrors.ErrCodeRateLimit) {
		t.Fatalf("expected rate limit error, got %v", err)
	}
}

func TestGetFundingCandles_ClassifiesDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`invalid-json`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL + "/",
	}

	_, err := client.GetFundingCandles("fUSD", "5m", 12)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), internalerrors.ErrCodeAPIDecode) {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestGetFundingCandles_ClassifiesTimeoutError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
		baseURL:    server.URL + "/",
	}

	_, err := client.GetFundingCandles("fUSD", "5m", 12)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), internalerrors.ErrCodeAPITimeout) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestGetCurrentFundingRate_UsesTickerEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/ticker/fUSD" {
			t.Fatalf("expected /ticker/fUSD path, got %s", got)
		}
		_, _ = w.Write([]byte(`[0.000321,0.00032,2,100,0.00033,30,100,0,0,0,0,0]`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		baseURL:    server.URL + "/",
	}

	rate, err := client.GetCurrentFundingRate("fUSD")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if rate != 0.000321 {
		t.Fatalf("expected FRR 0.000321, got %f", rate)
	}
}

func TestClassifyBitfinexHTTPStatusError_UsesHTTPStatusCode(t *testing.T) {
	err := classifyBitfinexHTTPStatusError("ticker/fUSD", http.StatusBadGateway, []byte("upstream error"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), internalerrors.ErrCodeAPIHTTPStatus) {
		t.Fatalf("expected API HTTP status error, got %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("expected HTTP status in error, got %v", err)
	}
}

func TestClassifyBitfinexError_ClassifiesRateLimitString(t *testing.T) {
	err := classifyBitfinexError("failed", fmt.Errorf("ratelimit: error"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), internalerrors.ErrCodeRateLimit) {
		t.Fatalf("expected rate limit error, got %v", err)
	}
}

func TestClassifyBitfinexError_ClassifiesAuthenticationStrings(t *testing.T) {
	testCases := []string{
		"apikey invalid (10100)",
		"digest invalid (10100)",
		"nonce: small (10114)",
	}

	for _, input := range testCases {
		err := classifyBitfinexError("failed", fmt.Errorf(input))
		if err == nil {
			t.Fatalf("expected error for %q", input)
		}
		if !strings.Contains(err.Error(), internalerrors.ErrCodeAuthentication) {
			t.Fatalf("expected auth error for %q, got %v", input, err)
		}
	}
}

func TestRetryPrivateRead_RetriesTimeoutOnceThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	client := &Client{}

	err := client.retryPrivateRead("get wallets", func() error {
		if attempts.Add(1) == 1 {
			return fmt.Errorf("context deadline exceeded")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestRetryPrivateRead_DoesNotRetryNonTimeoutError(t *testing.T) {
	var attempts atomic.Int32
	client := &Client{}

	err := client.retryPrivateRead("get wallets", func() error {
		attempts.Add(1)
		return fmt.Errorf("apikey invalid (10100)")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
	}
	if !strings.Contains(err.Error(), internalerrors.ErrCodeAuthentication) {
		t.Fatalf("expected auth classification for non-timeout auth error, got %v", err)
	}
}

func TestRetryPrivateRead_ReturnsRetriedTimeoutMessage(t *testing.T) {
	var attempts atomic.Int32
	client := &Client{}

	err := client.retryPrivateRead("get funding credits", func() error {
		attempts.Add(1)
		return fmt.Errorf("context deadline exceeded")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
	if !strings.Contains(err.Error(), "after 2 attempt(s)") {
		t.Fatalf("expected retry context in error, got %v", err)
	}
	if !strings.Contains(err.Error(), internalerrors.ErrCodeAPITimeout) {
		t.Fatalf("expected timeout classification, got %v", err)
	}
}

func TestClientExposesFundingCreditsHistoryAndLedgers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/r/funding/credits/fUSD/hist":
			_, _ = w.Write([]byte(`[
				[4951218878,"fUSD",1,1716265010000,1716453094000,437.0,null,"CLOSED","FIXED",null,null,0.00039,120,1716270000000,1716356400000,0,0,0,0,0.00039,0,null]
			]`))
		case "/auth/r/ledgers/USD/hist":
			var payload map[string]any
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read ledger request body: %v", err)
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("failed to decode ledger request body: %v", err)
			}
			if got := payload["limit"]; got != float64(2) {
				t.Fatalf("expected limit=2 in body, got %#v", got)
			}
			if got := payload["start"]; got != float64(1716422400000) {
				t.Fatalf("expected start in body, got %#v", got)
			}
			if got := payload["end"]; got != float64(1716508799000) {
				t.Fatalf("expected end in body, got %#v", got)
			}
			_, _ = w.Write([]byte(`[
				[123456,"USD",null,1716456900000,null,2.70,1002.70,null,"Interest Payment"]
			]`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	restClient := rest.NewClientWithURLHttpDo(server.URL+"/", func(c *http.Client, req *http.Request) (*http.Response, error) {
		return server.Client().Do(req)
	}).Credentials("key", "secret")

	client := &Client{
		restClient: restClient,
		httpClient: server.Client(),
		baseURL:    server.URL + "/",
	}

	credits, err := client.GetFundingCreditsHistory("fUSD")
	if err != nil {
		t.Fatalf("expected no credit history error, got %v", err)
	}
	if len(credits) != 1 {
		t.Fatalf("expected 1 credit history item, got %d", len(credits))
	}
	if credits[0].ID != 4951218878 || credits[0].Status != "CLOSED" {
		t.Fatalf("unexpected credit history item: %+v", credits[0])
	}

	ledgers, err := client.GetLedgers("USD", 1716422400000, 1716508799000, 2)
	if err != nil {
		t.Fatalf("expected no ledgers error, got %v", err)
	}
	if len(ledgers) != 1 {
		t.Fatalf("expected 1 ledger item, got %d", len(ledgers))
	}
	if ledgers[0].Amount != 2.70 || ledgers[0].Description != "Interest Payment" {
		t.Fatalf("unexpected ledger item: %+v", ledgers[0])
	}
}

func TestClientGetLedgersFiltered_SendsWalletAndCategoryBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/r/ledgers/USD/hist" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var payload map[string]any
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if got := payload["wallet"]; got != "funding" {
			t.Fatalf("expected wallet=funding, got %#v", got)
		}
		if got := payload["category"]; got != float64(28) {
			t.Fatalf("expected category=28, got %#v", got)
		}

		_, _ = w.Write([]byte(`[
			[123456,"USD",null,1716456900000,null,2.70,1002.70,null,"Interest Payment"]
		]`))
	}))
	defer server.Close()

	restClient := rest.NewClientWithURLHttpDo(server.URL+"/", func(c *http.Client, req *http.Request) (*http.Response, error) {
		return server.Client().Do(req)
	}).Credentials("key", "secret")

	client := &Client{
		restClient: restClient,
		httpClient: server.Client(),
		baseURL:    server.URL + "/",
	}

	entries, err := client.GetLedgersFiltered("USD", 1716422400000, 1716508799000, 2, "funding", int32(28))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(entries))
	}
}
