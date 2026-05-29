package config

import (
	"sort"
	"strings"
	"sync"

	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/errors"
)

// RuntimeConfigService 负责运行时配置的并发安全读写与统一校验。
type RuntimeConfigService struct {
	mu     sync.RWMutex
	config *Config
}

var runtimeMutableConfigKeys = []string{
	"HIGH_HOLD_AMOUNT",
	"HIGH_HOLD_ORDERS",
	"HIGH_HOLD_RATE",
	"KLINE_SMOOTH_METHOD",
	"LOAN_DAYS",
	"MAX_LOAN",
	"MIN_DAILY_LEND_RATE",
	"MIN_LOAN",
	"NOTIFY_RATE_THRESHOLD",
	"ORDER_LIMIT",
	"RATE_RANGE_INCREASE_PERCENT",
	"RESERVE_AMOUNT",
	"STRATEGY",
}

var startupOnlyConfigKeys = []string{
	"BITFINEX_API_KEY",
	"BITFINEX_SECRET_KEY",
	"CURRENCY",
	"FUNDING_BOOK_RATE_UNDERCUT",
	"GAP_BOTTOM",
	"GAP_TOP",
	"INCLUDE_MANUAL_PENDING_OFFERS_IN_STRATEGY_FUNDS",
	"KLINE_PERIOD",
	"KLINE_SPREAD_PERCENT",
	"KLINE_TIME_FRAME",
	"LENDING_CHECK_MINUTES",
	"DAILY_EARNINGS_REPORT",
	"LOAN_PERIOD_THRESHOLDS",
	"MINUTES_RUN",
	"NOTIFICATION_FORMAT",
	"RUN_ONLY_ON_NEW_CREDITS",
	"TELEGRAM_AUTH_TOKEN",
	"TELEGRAM_BOT_TOKEN",
	"TEST_MODE",
	"VOLATILITY_THRESHOLD",
	"MAX_RATE_MULTIPLIER",
	"MIN_RATE_MULTIPLIER",
}

func NewRuntimeConfigService(cfg *Config) *RuntimeConfigService {
	return &RuntimeConfigService{config: cfg}
}

func RuntimeMutableConfigKeys() []string {
	keys := make([]string, len(runtimeMutableConfigKeys))
	copy(keys, runtimeMutableConfigKeys)
	sort.Strings(keys)
	return keys
}

func StartupOnlyConfigKeys() []string {
	keys := make([]string, len(startupOnlyConfigKeys))
	copy(keys, startupOnlyConfigKeys)
	sort.Strings(keys)
	return keys
}

func (s *RuntimeConfigService) Config() *Config {
	return s.config
}

func (s *RuntimeConfigService) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := *s.config
	if s.config.LoanPeriodThresholds != nil {
		snapshot.LoanPeriodThresholds = make(map[int]float64, len(s.config.LoanPeriodThresholds))
		for k, v := range s.config.LoanPeriodThresholds {
			snapshot.LoanPeriodThresholds[k] = v
		}
	}
	if s.config.SeenFundingCreditIDs != nil {
		snapshot.SeenFundingCreditIDs = make(map[int64]struct{}, len(s.config.SeenFundingCreditIDs))
		for k, v := range s.config.SeenFundingCreditIDs {
			snapshot.SeenFundingCreditIDs[k] = v
		}
	}
	return snapshot
}

func (s *RuntimeConfigService) SetNotifyRateThreshold(threshold float64) error {
	if threshold <= 0 {
		return errors.NewValidationError("NOTIFY_RATE_THRESHOLD must be positive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.NotifyRateThreshold = threshold
	return nil
}

func (s *RuntimeConfigService) SetReserveAmount(amount float64) error {
	if amount < 0 {
		return errors.NewValidationError("RESERVE_AMOUNT must be non-negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.ReserveAmount = amount
	return nil
}

func (s *RuntimeConfigService) SetOrderLimit(limit int) error {
	if limit < 0 {
		return errors.NewValidationError("ORDER_LIMIT must be non-negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.OrderLimit = limit
	return nil
}

func (s *RuntimeConfigService) SetLoanDays(days int) error {
	if days < 0 {
		return errors.NewValidationError("LOAN_DAYS cannot be negative")
	}
	if days == 1 || days > constants.Period120Days {
		return errors.NewValidationError("LOAN_DAYS must be 0 or between 2 and 120")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.LoanDays = days
	return nil
}

func (s *RuntimeConfigService) SetMinDailyLendRate(value any) error {
	snapshot := s.Snapshot()
	snapshot.MinDailyLendRate = value
	minDailyRate, useFRR, err := snapshot.parseMinDailyLendRate()
	if err != nil {
		return err
	}
	if !useFRR && minDailyRate <= 0 {
		return errors.NewValidationError("MIN_DAILY_LEND_RATE must be positive or FRR")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.MinDailyLendRate = value
	return nil
}

func (s *RuntimeConfigService) SetMinLoan(amount float64) error {
	if amount <= 0 {
		return errors.NewValidationError("MIN_LOAN must be positive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MaxLoan > 0 && amount > s.config.MaxLoan {
		return errors.NewValidationError("MIN_LOAN cannot be greater than MAX_LOAN")
	}
	s.config.MinLoan = amount
	return nil
}

func (s *RuntimeConfigService) SetMaxLoan(amount float64) error {
	if amount < 0 {
		return errors.NewValidationError("MAX_LOAN must be non-negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if amount > 0 && amount < s.config.MinLoan {
		return errors.NewValidationError("MAX_LOAN cannot be less than MIN_LOAN")
	}
	s.config.MaxLoan = amount
	return nil
}

func (s *RuntimeConfigService) SetHighHoldRate(rate float64) error {
	if rate <= 0 {
		return errors.NewValidationError("HIGH_HOLD_RATE must be positive")
	}
	if rate > 7 {
		return errors.NewValidationError("HIGH_HOLD_RATE must be between 0 and 7")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.HighHoldRate = rate
	return nil
}

func (s *RuntimeConfigService) SetHighHoldAmount(amount float64) error {
	if amount < 0 {
		return errors.NewValidationError("HIGH_HOLD_AMOUNT must be non-negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.HighHoldAmount = amount
	return nil
}

func (s *RuntimeConfigService) SetHighHoldOrders(orders int) error {
	if orders < 1 {
		return errors.NewValidationError("HIGH_HOLD_ORDERS must be positive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.HighHoldOrders = orders
	return nil
}

func (s *RuntimeConfigService) SetRateRangeIncreasePercent(value float64) error {
	if value <= 0 || value > 1.0 {
		return errors.NewValidationError("RATE_RANGE_INCREASE_PERCENT must be between 0 and 1.0")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.RateRangeIncreasePercent = value
	return nil
}

func (s *RuntimeConfigService) SetStrategy(strategy string) error {
	normalized := strings.ToLower(strings.TrimSpace(strategy))
	switch normalized {
	case StrategyTraditional, StrategySimple, StrategySmart, StrategyKline:
	default:
		return errors.NewValidationError("STRATEGY must be one of: traditional, simple, smart, kline")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Strategy = normalized
	return nil
}

func (s *RuntimeConfigService) SetKlineSmoothMethod(method string) error {
	normalized := strings.ToLower(strings.TrimSpace(method))
	switch normalized {
	case "max", "sma", "ema", "hla", "p90":
	default:
		return errors.NewValidationError("KLINE_SMOOTH_METHOD must be one of: max, sma, ema, hla, p90")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.KlineSmoothMethod = normalized
	return nil
}

func (s *RuntimeConfigService) GetLendingCheckState() (int64, float64, map[int64]struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[int64]struct{}, len(s.config.SeenFundingCreditIDs))
	for k, v := range s.config.SeenFundingCreditIDs {
		seen[k] = v
	}
	return s.config.LastLendingCheckTime, s.config.LastAvailableBalance, seen
}

func (s *RuntimeConfigService) UpdateLendingCheckState(checkTime int64, balance float64, seen map[int64]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.LastLendingCheckTime = checkTime
	s.config.LastAvailableBalance = balance
	s.config.SeenFundingCreditIDs = seen
}
