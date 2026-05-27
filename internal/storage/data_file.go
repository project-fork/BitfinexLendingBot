package storage

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const DataFileName = "data.json"

var dataFileMu sync.Mutex

type Data struct {
	TrackedOrders map[int64]time.Time `json:"tracked_orders,omitempty"`
	Telegram      TelegramData        `json:"telegram,omitempty"`
	RuntimeConfig RuntimeConfigData   `json:"runtime_config,omitempty"`
	MarketHistory MarketHistoryData   `json:"market_history,omitempty"`
}

type TelegramData struct {
	AuthenticatedChatID int64 `json:"authenticated_chat_id,omitempty"`
}

type RuntimeConfigData struct {
	NotifyRateThreshold      *float64 `json:"notify_rate_threshold,omitempty"`
	ReserveAmount            *float64 `json:"reserve_amount,omitempty"`
	OrderLimit               *int     `json:"order_limit,omitempty"`
	LoanDays                 *int     `json:"loan_days,omitempty"`
	MinDailyLendRate         *string  `json:"min_daily_lend_rate,omitempty"`
	MinLoan                  *float64 `json:"min_loan,omitempty"`
	MaxLoan                  *float64 `json:"max_loan,omitempty"`
	HighHoldRate             *float64 `json:"high_hold_rate,omitempty"`
	HighHoldAmount           *float64 `json:"high_hold_amount,omitempty"`
	HighHoldOrders           *int     `json:"high_hold_orders,omitempty"`
	RateRangeIncreasePercent *float64 `json:"rate_range_increase_percent,omitempty"`
	Strategy                 *string  `json:"strategy,omitempty"`
	KlineSmoothMethod        *string  `json:"kline_smooth_method,omitempty"`
}

type MarketHistoryData struct {
	FundingSymbol string               `json:"funding_symbol,omitempty"`
	Snapshots     []MarketSnapshotData `json:"snapshots,omitempty"`
}

type MarketSnapshotData struct {
	Rate      float64   `json:"rate"`
	Volume    float64   `json:"volume"`
	Timestamp time.Time `json:"timestamp"`
}

func DefaultDataFilePath() string {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("无法取得执行档路径，使用目前目录保存 %s: %v", DataFileName, err)
		return DataFileName
	}
	return filepath.Join(filepath.Dir(exePath), DataFileName)
}

func LoadData(path string) Data {
	dataFileMu.Lock()
	defer dataFileMu.Unlock()
	return loadDataUnlocked(path)
}

func UpdateData(path string, update func(*Data)) error {
	if path == "" {
		return nil
	}

	dataFileMu.Lock()
	defer dataFileMu.Unlock()

	state := loadDataUnlocked(path)
	update(&state)
	return saveDataUnlocked(path, state)
}

func SaveData(path string, state Data) {
	dataFileMu.Lock()
	defer dataFileMu.Unlock()

	if err := saveDataUnlocked(path, state); err != nil {
		log.Printf("保存持久化资料失败: %v", err)
	}
}

func loadDataUnlocked(path string) Data {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("读取持久化资料失败，将使用空资料: %v", err)
		}
		return Data{}
	}

	var state Data
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("解析持久化资料失败，将使用空资料: %v", err)
		return Data{}
	}
	return state
}

func saveDataUnlocked(path string, state Data) error {
	if path == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tempFile := path + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tempFile, path); err != nil {
		_ = os.Remove(tempFile)
		return err
	}
	return nil
}
