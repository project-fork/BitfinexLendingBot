package storage

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

const DataFileName = "data.json"

type Data struct {
	TrackedOrders map[int64]time.Time `json:"tracked_orders,omitempty"`
	Telegram      TelegramData        `json:"telegram,omitempty"`
}

type TelegramData struct {
	AuthenticatedChatID int64 `json:"authenticated_chat_id,omitempty"`
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

func SaveData(path string, state Data) {
	if path == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Printf("建立持久化资料目录失败: %v", err)
		return
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		log.Printf("序列化持久化资料失败: %v", err)
		return
	}
	data = append(data, '\n')

	tempFile := path + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		log.Printf("写入持久化资料失败: %v", err)
		return
	}
	if err := os.Rename(tempFile, path); err != nil {
		log.Printf("更新持久化资料失败: %v", err)
		_ = os.Remove(tempFile)
	}
}
