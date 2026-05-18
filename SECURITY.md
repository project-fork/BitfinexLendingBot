# 🔐 安全配置指南

## ⚠️ 重要安全提醒

**切勿将 API 密钥提交到版本控制系统！**

## 🛡️ 安全配置设置

### 方法 1: 配置文件（推荐）

1. 复制示例配置文件：
   ```bash
   cp config.yaml.example config.yaml
   ```

2. 编辑 `config.yaml` 填入你的真实 API 密钥：
   ```yaml
   BITFINEX_API_KEY: "your_actual_api_key_here"
   BITFINEX_SECRET_KEY: "your_actual_secret_key_here"
   TELEGRAM_BOT_TOKEN: "your_actual_telegram_bot_token"
   TELEGRAM_AUTH_TOKEN: "your_secure_password_123"
   ```

3. 确保 `config.yaml` 已在 `.gitignore` 中被忽略

### 方法 2: 环境变量（更安全）

设置环境变量：
```bash
export BITFINEX_API_KEY="your_actual_api_key_here"
export BITFINEX_SECRET_KEY="your_actual_secret_key_here"
export TELEGRAM_BOT_TOKEN="your_actual_telegram_bot_token"
export TELEGRAM_AUTH_TOKEN="your_secure_password_123"
```

程序会自动读取环境变量，环境变量的优先级高于配置文件。

### 方法 3: .env 文件

创建 `.env` 文件（也会被 `.gitignore` 忽略）：
```bash
BITFINEX_API_KEY=your_actual_api_key_here
BITFINEX_SECRET_KEY=your_actual_secret_key_here
TELEGRAM_BOT_TOKEN=your_actual_telegram_bot_token
TELEGRAM_AUTH_TOKEN=your_secure_password_123
```

## 🔒 API 权限设置

在 Bitfinex 创建 API 密钥时，请确保：

1. **权限设置**：
   - ✅ 查看钱包余额
   - ✅ 贷出资金
   - ✅ 取消贷出订单
   - ❌ 不需要提现权限

2. **IP 限制**（强烈建议）：
   - 限制只能从特定 IP 地址访问

3. **定期更换**：
   - 建议定期更换 API 密钥

## 🚨 如果 API 密钥泄露

1. **立即撤销** Bitfinex 上的 API 密钥
2. **检查帐户** 是否有异常活动
3. **创建新的** API 密钥对
4. **检查版本控制历史** 是否有敏感信息提交记录

## 📋 安全检查清单

- [ ] API 密钥未硬编码在代码中
- [ ] `config.yaml` 在 `.gitignore` 中
- [ ] 使用强密码作为 Telegram 认证 token
- [ ] API 密钥设置了适当的权限
- [ ] 考虑使用 IP 限制
- [ ] 定期检查和更换密钥

## 🛠️ 测试安全配置

运行程序前先测试配置：
```bash
# 使用测试模式验证配置
TEST_MODE=true ./bitfinex-lending-bot
```

如果看到类似错误，说明配置需要更新：
```
Error: BITFINEX_API_KEY is required and must be set to your actual API key
```

## 📞 问题回报

如果发现安全问题，请通过私人管道联系维护者，不要在公开的 issue 中报告安全漏洞。