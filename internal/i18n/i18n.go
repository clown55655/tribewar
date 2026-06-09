package i18n

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"tribeway/internal/logger"
)

// I18nManager 国际化管理器
type I18nManager struct {
	bundle       *i18n.Bundle
	localizers   map[string]*i18n.Localizer
	languages    []string
	defaultLang  string
	translations map[string]map[string]string
	mutex        sync.RWMutex
}

// LanguageConfig 语言配置
type LanguageConfig struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Default bool   `json:"default"`
}

// Translation 翻译项
type Translation struct {
	ID           string            `json:"id"`
	Description  string            `json:"description,omitempty"`
	One          string            `json:"one,omitempty"`
	Other        string            `json:"other,omitempty"`
	Translations map[string]string `json:"translations,omitempty"`
}

// NewI18nManager 创建国际化管理器
func NewI18nManager(defaultLang string) *I18nManager {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	manager := &I18nManager{
		bundle:       bundle,
		localizers:   make(map[string]*i18n.Localizer),
		languages:    make([]string, 0),
		defaultLang:  defaultLang,
		translations: make(map[string]map[string]string),
	}

	// 加载默认语言
	manager.LoadLanguage(defaultLang)

	logger.Info(fmt.Sprintf("I18n manager initialized with default language: %s", defaultLang))
	return manager
}

// LoadLanguage 加载语言包
func (im *I18nManager) LoadLanguage(langCode string) error {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	// 解析语言标签
	_, err := language.Parse(langCode)
	if err != nil {
		return fmt.Errorf("invalid language code: %s", langCode)
	}

	// 加载语言文件
	langFile := filepath.Join("locales", fmt.Sprintf("%s.json", langCode))
	messageFile, err := im.bundle.LoadMessageFile(langFile)
	if err != nil {
		// 如果文件不存在，创建默认的语言文件
		if err := im.createDefaultLanguageFile(langCode); err != nil {
			return fmt.Errorf("failed to create default language file: %v", err)
		}

		messageFile, err = im.bundle.LoadMessageFile(langFile)
		if err != nil {
			return fmt.Errorf("failed to load language file: %v", err)
		}
	}

	// 创建本地化器
	localizer := i18n.NewLocalizer(im.bundle, langCode)
	im.localizers[langCode] = localizer

	// 添加到支持语言列表
	if !contains(im.languages, langCode) {
		im.languages = append(im.languages, langCode)
	}

	logger.Info(fmt.Sprintf("Loaded language: %s (%d messages)",
		langCode, len(messageFile.Messages)))

	return nil
}

// createDefaultLanguageFile 创建默认语言文件
func (im *I18nManager) createDefaultLanguageFile(langCode string) error {
	translations := im.getDefaultTranslations(langCode)

	data, err := json.MarshalIndent(translations, "", "  ")
	if err != nil {
		return err
	}

	langDir := "locales"
	if err := os.MkdirAll(langDir, 0755); err != nil {
		return err
	}

	langFile := filepath.Join(langDir, fmt.Sprintf("%s.json", langCode))
	return ioutil.WriteFile(langFile, data, 0644)
}

// getDefaultTranslations 获取默认翻译
func (im *I18nManager) getDefaultTranslations(langCode string) []Translation {
	translations := []Translation{
		{ID: "error.invalid_username", One: "Invalid username"},
		{ID: "error.invalid_password", One: "Invalid password"},
		{ID: "error.user_not_found", One: "User not found"},
		{ID: "error.user_already_exists", One: "User already exists"},
		{ID: "error.login_failed", One: "Login failed"},
		{ID: "error.permission_denied", One: "Permission denied"},
		{ID: "error.server_error", One: "Server error"},
		{ID: "error.rate_limit_exceeded", One: "Rate limit exceeded"},

		{ID: "success.login", One: "Login successful"},
		{ID: "success.logout", One: "Logout successful"},
		{ID: "success.register", One: "Registration successful"},
		{ID: "success.room_created", One: "Room created"},
		{ID: "success.game_started", One: "Game started"},
		{ID: "success.friend_added", One: "Friend added"},

		{ID: "game.waiting_for_players", One: "Waiting for players"},
		{ID: "game.game_started", One: "Game started"},
		{ID: "game.game_ended", One: "Game ended"},
		{ID: "game.your_turn", One: "Your turn"},
		{ID: "game.waiting_for_opponent", One: "Waiting for opponent"},

		{ID: "chat.message_sent", One: "Message sent"},
		{ID: "chat.user_blocked", One: "User blocked"},
		{ID: "chat.message_too_long", One: "Message too long"},

		{ID: "friend.request_sent", One: "Friend request sent"},
		{ID: "friend.request_accepted", One: "Friend request accepted"},
		{ID: "friend.already_friends", One: "Already friends"},

		{ID: "mail.new_mail", One: "New mail received"},
		{ID: "mail.rewards_claimed", One: "Rewards claimed"},
		{ID: "mail.mail_deleted", One: "Mail deleted"},
	}

	// 根据语言代码添加特定翻译
	switch langCode {
	case "zh-CN":
		im.addChineseTranslations(translations)
	case "ja":
		im.addJapaneseTranslations(translations)
	case "ko":
		im.addKoreanTranslations(translations)
	}

	return translations
}

// addChineseTranslations 添加中文翻译
func (im *I18nManager) addChineseTranslations(translations []Translation) {
	chineseMap := map[string]string{
		"error.invalid_username":    "用户名无效",
		"error.invalid_password":    "密码无效",
		"error.user_not_found":      "用户不存在",
		"error.user_already_exists": "用户已存在",
		"error.login_failed":        "登录失败",
		"error.permission_denied":   "权限不足",
		"error.server_error":        "服务器错误",
		"error.rate_limit_exceeded": "请求过于频繁",

		"success.login":        "登录成功",
		"success.logout":       "登出成功",
		"success.register":     "注册成功",
		"success.room_created": "房间创建成功",
		"success.game_started": "游戏开始",
		"success.friend_added": "好友添加成功",

		"game.waiting_for_players":  "等待玩家加入",
		"game.game_started":         "游戏开始",
		"game.game_ended":           "游戏结束",
		"game.your_turn":            "轮到你了",
		"game.waiting_for_opponent": "等待对手操作",

		"chat.message_sent":     "消息已发送",
		"chat.user_blocked":     "用户已屏蔽",
		"chat.message_too_long": "消息过长",

		"friend.request_sent":     "好友请求已发送",
		"friend.request_accepted": "好友请求已接受",
		"friend.already_friends":  "已经是好友了",

		"mail.new_mail":        "收到新邮件",
		"mail.rewards_claimed": "奖励已领取",
		"mail.mail_deleted":    "邮件已删除",
	}

	for i, translation := range translations {
		if chinese, exists := chineseMap[translation.ID]; exists {
			translations[i].One = chinese
		}
	}
}

// addJapaneseTranslations 添加日文翻译
func (im *I18nManager) addJapaneseTranslations(translations []Translation) {
	japaneseMap := map[string]string{
		"error.invalid_username": "無効なユーザー名",
		"error.invalid_password": "無効なパスワード",
		"error.user_not_found":   "ユーザーが見つかりません",
		"error.login_failed":     "ログインに失敗しました",
		"success.login":          "ログイン成功",
		"game.game_started":      "ゲーム開始",
		"game.your_turn":         "あなたのターンです",
	}

	for i, translation := range translations {
		if japanese, exists := japaneseMap[translation.ID]; exists {
			translations[i].One = japanese
		}
	}
}

// addKoreanTranslations 添加韩文翻译
func (im *I18nManager) addKoreanTranslations(translations []Translation) {
	koreanMap := map[string]string{
		"error.invalid_username": "잘못된 사용자명",
		"error.invalid_password": "잘못된 비밀번호",
		"error.user_not_found":   "사용자를 찾을 수 없습니다",
		"error.login_failed":     "로그인 실패",
		"success.login":          "로그인 성공",
		"game.game_started":      "게임 시작",
		"game.your_turn":         "당신의 차례입니다",
	}

	for i, translation := range translations {
		if korean, exists := koreanMap[translation.ID]; exists {
			translations[i].One = korean
		}
	}
}

// Translate 翻译文本
func (im *I18nManager) Translate(langCode, messageID string, templateData map[string]interface{}) string {
	im.mutex.RLock()
	localizer, exists := im.localizers[langCode]
	im.mutex.RUnlock()

	if !exists {
		// 使用默认语言
		localizer = im.localizers[im.defaultLang]
		if localizer == nil {
			return messageID // 返回消息ID作为后备
		}
	}

	config := &i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: templateData,
	}

	translation, err := localizer.Localize(config)
	if err != nil {
		logger.Debug(fmt.Sprintf("Translation not found: %s for %s", messageID, langCode))
		return messageID
	}

	return translation
}

// GetSupportedLanguages 获取支持的语言列表
func (im *I18nManager) GetSupportedLanguages() []string {
	im.mutex.RLock()
	defer im.mutex.RUnlock()

	languages := make([]string, len(im.languages))
	copy(languages, im.languages)
	return languages
}

// DetectLanguage 检测客户端语言
func (im *I18nManager) DetectLanguage(acceptLanguage string) string {
	if acceptLanguage == "" {
		return im.defaultLang
	}

	// 解析Accept-Language头
	languages := parseAcceptLanguage(acceptLanguage)

	// 找到支持的语言
	for _, lang := range languages {
		for _, supported := range im.languages {
			if lang == supported {
				return lang
			}

			// 检查语言系列匹配（例如 zh-CN 匹配 zh）
			if strings.HasPrefix(lang, supported) || strings.HasPrefix(supported, lang) {
				return supported
			}
		}
	}

	return im.defaultLang
}

// parseAcceptLanguage 解析Accept-Language头
func parseAcceptLanguage(acceptLanguage string) []string {
	languages := make([]string, 0)

	parts := strings.Split(acceptLanguage, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		// 移除质量值（q=0.8）
		if idx := strings.Index(part, ";"); idx != -1 {
			part = part[:idx]
		}

		part = strings.TrimSpace(part)
		if part != "" {
			languages = append(languages, part)
		}
	}

	return languages
}

// UpdateTranslation 更新翻译
func (im *I18nManager) UpdateTranslation(langCode, messageID, translation string) error {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	if im.translations[langCode] == nil {
		im.translations[langCode] = make(map[string]string)
	}

	im.translations[langCode][messageID] = translation

	// 重新加载本地化器
	return im.reloadLocalizer(langCode)
}

// reloadLocalizer 重新加载本地化器
func (im *I18nManager) reloadLocalizer(langCode string) error {
	// 重新创建本地化器
	localizer := i18n.NewLocalizer(im.bundle, langCode)
	im.localizers[langCode] = localizer

	logger.Debug(fmt.Sprintf("Reloaded localizer for language: %s", langCode))
	return nil
}

// GetTranslationKeys 获取所有翻译键
func (im *I18nManager) GetTranslationKeys(langCode string) []string {
	im.mutex.RLock()
	defer im.mutex.RUnlock()

	translations, exists := im.translations[langCode]
	if !exists {
		return nil
	}

	keys := make([]string, 0, len(translations))
	for key := range translations {
		keys = append(keys, key)
	}

	return keys
}

// ExportTranslations 导出翻译
func (im *I18nManager) ExportTranslations(langCode string) (map[string]string, error) {
	im.mutex.RLock()
	defer im.mutex.RUnlock()

	translations, exists := im.translations[langCode]
	if !exists {
		return nil, fmt.Errorf("language not found: %s", langCode)
	}

	// 创建副本
	exported := make(map[string]string)
	for key, value := range translations {
		exported[key] = value
	}

	return exported, nil
}

// ImportTranslations 导入翻译
func (im *I18nManager) ImportTranslations(langCode string, translations map[string]string) error {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	if im.translations[langCode] == nil {
		im.translations[langCode] = make(map[string]string)
	}

	// 合并翻译
	for key, value := range translations {
		im.translations[langCode][key] = value
	}

	// 重新加载本地化器
	return im.reloadLocalizer(langCode)
}

// ValidateTranslations 验证翻译完整性
func (im *I18nManager) ValidateTranslations() []string {
	im.mutex.RLock()
	defer im.mutex.RUnlock()

	var issues []string

	// 获取默认语言的所有键
	defaultTranslations := im.translations[im.defaultLang]
	if defaultTranslations == nil {
		issues = append(issues, fmt.Sprintf("Default language %s not found", im.defaultLang))
		return issues
	}

	// 检查其他语言的翻译完整性
	for langCode, translations := range im.translations {
		if langCode == im.defaultLang {
			continue
		}

		for key := range defaultTranslations {
			if _, exists := translations[key]; !exists {
				issues = append(issues, fmt.Sprintf("Missing translation: %s in %s", key, langCode))
			}
		}
	}

	return issues
}

// LocalizedError 本地化错误
type LocalizedError struct {
	MessageID string
	LangCode  string
	manager   *I18nManager
	data      map[string]interface{}
}

// NewLocalizedError 创建本地化错误
func NewLocalizedError(manager *I18nManager, langCode, messageID string, data map[string]interface{}) *LocalizedError {
	return &LocalizedError{
		MessageID: messageID,
		LangCode:  langCode,
		manager:   manager,
		data:      data,
	}
}

// Error 实现error接口
func (le *LocalizedError) Error() string {
	return le.manager.Translate(le.LangCode, le.MessageID, le.data)
}

// GetMessageID 获取消息ID
func (le *LocalizedError) GetMessageID() string {
	return le.MessageID
}

// GetLangCode 获取语言代码
func (le *LocalizedError) GetLangCode() string {
	return le.LangCode
}

// NumberLocalizer 数字本地化器
type NumberLocalizer struct {
	printers map[string]*message.Printer
	mutex    sync.RWMutex
}

// NewNumberLocalizer 创建数字本地化器
func NewNumberLocalizer() *NumberLocalizer {
	return &NumberLocalizer{
		printers: make(map[string]*message.Printer),
	}
}

// FormatNumber 格式化数字
func (nl *NumberLocalizer) FormatNumber(langCode string, number interface{}) string {
	nl.mutex.RLock()
	printer, exists := nl.printers[langCode]
	nl.mutex.RUnlock()

	if !exists {
		// 创建新的打印器
		tag, err := language.Parse(langCode)
		if err != nil {
			tag = language.English
		}

		printer = message.NewPrinter(tag)

		nl.mutex.Lock()
		nl.printers[langCode] = printer
		nl.mutex.Unlock()
	}

	return printer.Sprintf("%v", number)
}

// FormatCurrency 格式化货币
func (nl *NumberLocalizer) FormatCurrency(langCode string, amount int64, currency string) string {
	formatted := nl.FormatNumber(langCode, amount)

	switch langCode {
	case "zh-CN":
		return fmt.Sprintf("%s%s", formatted, getCurrencySymbol(currency, "zh-CN"))
	case "ja":
		return fmt.Sprintf("%s%s", formatted, getCurrencySymbol(currency, "ja"))
	default:
		return fmt.Sprintf("%s %s", getCurrencySymbol(currency, "en"), formatted)
	}
}

// getCurrencySymbol 获取货币符号
func getCurrencySymbol(currency, langCode string) string {
	symbols := map[string]map[string]string{
		"gold": {
			"en":    "Gold",
			"zh-CN": "金币",
			"ja":    "ゴールド",
		},
		"diamond": {
			"en":    "Diamond",
			"zh-CN": "钻石",
			"ja":    "ダイヤ",
		},
	}

	if currencySymbols, exists := symbols[currency]; exists {
		if symbol, exists := currencySymbols[langCode]; exists {
			return symbol
		}
		return currencySymbols["en"] // 默认英文
	}

	return currency
}

// TimeLocalizer 时间本地化器
type TimeLocalizer struct {
	formats map[string]string
}

// NewTimeLocalizer 创建时间本地化器
func NewTimeLocalizer() *TimeLocalizer {
	return &TimeLocalizer{
		formats: map[string]string{
			"en":    "2006-01-02 15:04:05",
			"zh-CN": "2006年01月02日 15:04:05",
			"ja":    "2006年01月02日 15:04:05",
			"ko":    "2006년 01월 02일 15:04:05",
		},
	}
}

// FormatTime 格式化时间
func (tl *TimeLocalizer) FormatTime(langCode string, t time.Time) string {
	format, exists := tl.formats[langCode]
	if !exists {
		format = tl.formats["en"]
	}

	return t.Format(format)
}

// FormatDuration 格式化持续时间
func (tl *TimeLocalizer) FormatDuration(langCode string, duration time.Duration) string {
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	switch langCode {
	case "zh-CN":
		if hours > 0 {
			return fmt.Sprintf("%d小时%d分钟%d秒", hours, minutes, seconds)
		} else if minutes > 0 {
			return fmt.Sprintf("%d分钟%d秒", minutes, seconds)
		} else {
			return fmt.Sprintf("%d秒", seconds)
		}
	case "ja":
		if hours > 0 {
			return fmt.Sprintf("%d時間%d分%d秒", hours, minutes, seconds)
		} else if minutes > 0 {
			return fmt.Sprintf("%d分%d秒", minutes, seconds)
		} else {
			return fmt.Sprintf("%d秒", seconds)
		}
	default:
		if hours > 0 {
			return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
		} else if minutes > 0 {
			return fmt.Sprintf("%dm %ds", minutes, seconds)
		} else {
			return fmt.Sprintf("%ds", seconds)
		}
	}
}

// contains 检查切片是否包含元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
