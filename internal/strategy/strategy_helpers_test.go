package strategy

import (
	"reflect"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestBuildTraditionalDepthProgressionIndexes_UsesUnifiedGapRange(t *testing.T) {
	cfg := &config.Config{
		GapBottom: 2,
		GapTop:    5,
	}
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00021},
		{Rate: 0.00022},
		{Rate: 0.00023},
		{Rate: 0.00024},
		{Rate: 0.00025},
		{Rate: 0.00026},
	}

	got := buildTraditionalDepthProgressionIndexes(cfg, fundingBook, 4)
	want := []int{2, 3, 4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestBuildTraditionalDepthProgressionIndexes_ClampsToBookRange(t *testing.T) {
	cfg := &config.Config{
		GapBottom: 10,
		GapTop:    5000,
	}
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00021},
		{Rate: 0.00022},
		{Rate: 0.00023},
		{Rate: 0.00024},
	}

	got := buildTraditionalDepthProgressionIndexes(cfg, fundingBook, 3)
	want := []int{3, 3, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
