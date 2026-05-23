package bitfinex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
