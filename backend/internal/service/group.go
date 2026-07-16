package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type OpenAIMessagesDispatchModelConfig = domain.OpenAIMessagesDispatchModelConfig
type GroupModelsListConfig = domain.GroupModelsListConfig
type PeakRateWindow = domain.PeakRateWindow

const MaxPeakRateWindows = 24

type Group struct {
	ID             int64
	Name           string
	Description    string
	Platform       string
	RateMultiplier float64
	// 高峰时段倍率：peak_rate_enabled 为 true 且当前时刻命中配置窗口时，
	// 所有计费模式的最终倍率都会额外乘以窗口倍率。详见 PeakMultiplierAt。
	PeakRateEnabled    bool
	PeakStart          string
	PeakEnd            string
	PeakRateMultiplier float64
	PeakRateWindows    []PeakRateWindow
	IsExclusive        bool
	Status             string
	Hydrated           bool // indicates the group was loaded from a trusted repository source
	// DuplicateOperationID is internal persistence metadata used only to recover
	// an already committed one-click copy. It must never be mapped to API DTOs.
	DuplicateOperationID string

	SubscriptionType    string
	DailyLimitUSD       *float64
	WeeklyLimitUSD      *float64
	MonthlyLimitUSD     *float64
	CustomLimitHours    int
	CustomLimitUSD      *float64
	DefaultValidityDays int

	// 图片生成计费配置（antigravity 和 gemini 平台使用）
	AllowImageGeneration         bool
	AllowBatchImageGeneration    bool
	ImageRateIndependent         bool
	ImageRateMultiplier          float64
	ImagePrice1K                 *float64
	ImagePrice2K                 *float64
	ImagePrice4K                 *float64
	BatchImageDiscountMultiplier float64
	BatchImageHoldMultiplier     float64
	VideoRateIndependent         bool
	VideoRateMultiplier          float64
	VideoPrice480P               *float64
	VideoPrice720P               *float64
	VideoPrice1080P              *float64
	// Codex alpha/search 网页搜索单次价格（USD/次，仅 openai 平台使用）；
	// nil 表示使用默认价 defaultWebSearchPricePerCall（官方 $10/1000 次）。
	WebSearchPricePerCall *float64

	// Claude Code 客户端限制
	ClaudeCodeOnly  bool
	FallbackGroupID *int64
	// 无效请求兜底分组（仅 anthropic 平台使用）
	FallbackGroupIDOnInvalidRequest *int64

	// 模型路由配置
	// key: 模型匹配模式（支持 * 通配符，如 "claude-opus-*"）
	// value: 优先账号 ID 列表
	ModelRouting        map[string][]int64
	ModelRoutingEnabled bool

	// MCP XML 协议注入开关（仅 antigravity 平台使用）
	MCPXMLInject bool

	// 支持的模型系列（仅 antigravity 平台使用）
	// 可选值: claude, gemini_text, gemini_image
	SupportedModelScopes []string

	// 分组排序
	SortOrder int

	// OpenAI Messages 调度配置（仅 openai 平台使用）
	AllowMessagesDispatch       bool
	RequireOAuthOnly            bool // 仅允许非 apikey 类型账号关联（OpenAI/Antigravity/Anthropic/Gemini）
	RequirePrivacySet           bool // 调度时仅允许 privacy 已成功设置的账号（OpenAI/Antigravity/Anthropic/Gemini）
	DefaultMappedModel          string
	MessagesDispatchModelConfig OpenAIMessagesDispatchModelConfig
	ModelsListConfig            GroupModelsListConfig

	// RPMLimit 分组级每分钟请求数上限（0 = 不限制）。
	// 一旦设置即接管该分组用户的限流（覆盖用户级 rpm_limit），可被 user-group rpm_override 进一步覆盖。
	RPMLimit int

	NewAPIStyleInterfaceEnabled bool

	CreatedAt time.Time
	UpdatedAt time.Time

	AccountGroups           []AccountGroup
	AccountCount            int64
	ActiveAccountCount      int64
	RateLimitedAccountCount int64
}

func (g *Group) IsActive() bool {
	return g.Status == StatusActive
}

func (g *Group) IsSubscriptionType() bool {
	return g.SubscriptionType == SubscriptionTypeSubscription
}

func (g *Group) HasDailyLimit() bool {
	return g.DailyLimitUSD != nil && *g.DailyLimitUSD > 0
}

func (g *Group) HasWeeklyLimit() bool {
	return g.WeeklyLimitUSD != nil && *g.WeeklyLimitUSD > 0
}

func (g *Group) HasMonthlyLimit() bool {
	return g.MonthlyLimitUSD != nil && *g.MonthlyLimitUSD > 0
}

func (g *Group) HasCustomLimit() bool {
	return g.CustomLimitHours > 0 && g.CustomLimitUSD != nil && *g.CustomLimitUSD > 0
}

// GetImagePrice 根据 image_size 返回对应的图片生成价格
// 如果分组未配置价格，返回 nil（调用方应使用默认值）
func (g *Group) GetImagePrice(imageSize string) *float64 {
	switch imageSize {
	case "1K":
		return g.ImagePrice1K
	case "2K":
		return g.ImagePrice2K
	case "4K":
		return g.ImagePrice4K
	default:
		// 未知尺寸默认按 2K 计费
		return g.ImagePrice2K
	}
}

// GetVideoPrice 根据 resolution 返回对应的视频生成价格。
// 如果分组未配置价格，返回 nil（调用方应使用默认值）。
func (g *Group) GetVideoPrice(resolution string) *float64 {
	switch NormalizeVideoBillingResolutionOrDefault(resolution) {
	case VideoBillingResolution480P:
		return g.VideoPrice480P
	case VideoBillingResolution720P:
		return g.VideoPrice720P
	case VideoBillingResolution1080P:
		return g.VideoPrice1080P
	default:
		return g.VideoPrice480P
	}
}

// IsGroupContextValid reports whether a group from context has the fields required for routing decisions.
func IsGroupContextValid(group *Group) bool {
	if group == nil {
		return false
	}
	if group.ID <= 0 {
		return false
	}
	if !group.Hydrated {
		return false
	}
	if group.Platform == "" || group.Status == "" {
		return false
	}
	return true
}

// GetRoutingAccountIDs 根据请求模型获取路由账号 ID 列表
// 返回匹配的优先账号 ID 列表，如果没有匹配规则则返回 nil
func (g *Group) GetRoutingAccountIDs(requestedModel string) []int64 {
	if !g.ModelRoutingEnabled || len(g.ModelRouting) == 0 || requestedModel == "" {
		return nil
	}

	// 1. 精确匹配优先
	if accountIDs, ok := g.ModelRouting[requestedModel]; ok && len(accountIDs) > 0 {
		return accountIDs
	}

	// 2. 通配符匹配（前缀匹配）
	for pattern, accountIDs := range g.ModelRouting {
		if matchModelPattern(pattern, requestedModel) && len(accountIDs) > 0 {
			return accountIDs
		}
	}

	return nil
}

// matchModelPattern 检查模型是否匹配模式
// 支持 * 通配符，如 "claude-opus-*" 匹配 "claude-opus-4-20250514"
func matchModelPattern(pattern, model string) bool {
	if pattern == model {
		return true
	}

	// 处理 * 通配符（仅支持末尾通配符）
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(model, prefix)
	}

	return false
}

// parseMinutes 把 "HH:MM" 解析为当日分钟数（0..1439），格式非法返回 (0,false)。
// 手工解析而非 time.Parse：本函数位于每请求的计费热路径（PeakMultiplierAt），
// 避免对静态配置字符串重复走 layout 解析与 time.Time 分配。
// 接受集与 time.Parse("15:04", s) 完全一致（存量数据按旧解析写入，不得收窄）：
// Hours and minutes must be zero-padded: HH:MM.
func parseMinutes(hhmm string) (int, bool) {
	colon := strings.IndexByte(hhmm, ':')
	if colon != 2 || len(hhmm) != 5 {
		return 0, false
	}
	h := 0
	for i := 0; i < colon; i++ {
		d := hhmm[i] - '0'
		if d > 9 {
			return 0, false
		}
		h = h*10 + int(d)
	}
	m1, m2 := hhmm[colon+1]-'0', hhmm[colon+2]-'0'
	if m1 > 9 || m2 > 9 {
		return 0, false
	}
	m := int(m1)*10 + int(m2)
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func parseLegacyMinutes(hhmm string) (int, string, bool) {
	hhmm = strings.TrimSpace(hhmm)
	colon := strings.IndexByte(hhmm, ':')
	if colon <= 0 || colon > 2 || len(hhmm)-colon-1 != 2 {
		return 0, "", false
	}
	h := 0
	for i := 0; i < colon; i++ {
		d := hhmm[i] - '0'
		if d > 9 {
			return 0, "", false
		}
		h = h*10 + int(d)
	}
	m1, m2 := hhmm[colon+1]-'0', hhmm[colon+2]-'0'
	if m1 > 9 || m2 > 9 {
		return 0, "", false
	}
	m := int(m1)*10 + int(m2)
	if h > 23 || m > 59 {
		return 0, "", false
	}
	return h*60 + m, fmt.Sprintf("%02d:%02d", h, m), true
}

// PeakMultiplierAt 返回指定时刻 now 的高峰因子。
//   - 未启用 / 未配置 / 配置非法（start>=end 或格式错误） / 非高峰时段 → 返回 1.0（安全降级）
//   - 区间为左闭右开 [PeakStart, PeakEnd)，仅支持当日区间，不支持跨天（如 22:00-次日02:00）
//   - 时刻基于全局系统时区（timezone.Location）判定
//
// 该方法是纯函数，不读取任何外部状态，便于单测。
func (g *Group) PeakMultiplierAt(now time.Time) float64 {
	if g == nil || !g.PeakRateEnabled {
		return 1.0
	}
	windows := normalizedPeakRateWindowsForRead(g.PeakRateWindows, g.PeakStart, g.PeakEnd, g.PeakRateMultiplier)
	t := now.In(timezone.Location())
	cur := t.Hour()*60 + t.Minute()
	for _, window := range windows {
		start, ok1 := parseMinutes(window.Start)
		end, ok2 := parseMinutes(window.End)
		if !ok1 || !ok2 || start >= end {
			continue
		}
		if cur >= start && cur < end {
			return window.Multiplier
		}
	}
	return 1.0
}

// ValidatePeakRateConfig 是高峰倍率配置的唯一校验来源，供 handler 与 service 层共用。
// enabled=true 时要求窗口合法且 end>start（不支持跨天），multiplier>=0。
// multiplier=0 是允许的，表示命中高峰窗口时按 0 倍计费，可用于折扣/免费策略。
// enabled=false 时放行（不关心类型）。subscriptionType 为空按 standard 处理。
func ValidatePeakRateConfig(subscriptionType string, enabled bool, start, end string, multiplier float64) error {
	_ = subscriptionType
	return ValidatePeakRateWindows(enabled, singlePeakRateWindowFromLegacy(start, end, multiplier))
}

func ValidatePeakRateWindows(enabled bool, windows []PeakRateWindow) error {
	_, err := NormalizePeakRateWindows(enabled, windows)
	return err
}

// NormalizePeakRateConfig 归一化最终落库的高峰配置，CreateGroup 与 UpdateGroup 两条写路径共用（唯一收口）：
//   - 标准余额分组和订阅分组都允许携带高峰配置；
//   - 关闭高峰或窗口非法时清空高峰配置，避免脏数据入库。
//
// 与 ValidatePeakRateConfig 的分工：enabled=true 时校验已保证各字段合法，本函数为无操作；
// enabled=false 时校验放行，由本函数兜底清洗。调用顺序为先归一化、后校验。
func NormalizePeakRateConfig(subscriptionType string, enabled bool, start, end string, multiplier float64) (bool, string, string, float64) {
	_ = subscriptionType
	windows, err := NormalizePeakRateWindows(enabled, singlePeakRateWindowFromLegacy(start, end, multiplier))
	if err != nil || !enabled || len(windows) == 0 {
		return false, "", "", 1.0
	}
	first := windows[0]
	return true, first.Start, first.End, first.Multiplier
}

func NormalizePeakRateWindows(enabled bool, windows []PeakRateWindow) ([]PeakRateWindow, error) {
	if !enabled {
		return nil, nil
	}
	if len(windows) == 0 {
		return nil, errors.New("peak_rate_enabled 为 true 时 peak_rate_windows 必填")
	}
	if len(windows) > MaxPeakRateWindows {
		return nil, fmt.Errorf("peak_rate_windows 最多允许 %d 段", MaxPeakRateWindows)
	}

	normalized := make([]PeakRateWindow, 0, len(windows))
	for _, window := range windows {
		window.Start = strings.TrimSpace(window.Start)
		window.End = strings.TrimSpace(window.End)
		st, canonicalStart, okStart := parseLegacyMinutes(window.Start)
		if !okStart {
			return nil, fmt.Errorf("peak window start 格式应为 HH:MM，got %q", window.Start)
		}
		en, canonicalEnd, okEnd := parseLegacyMinutes(window.End)
		if !okEnd {
			return nil, fmt.Errorf("peak window end 格式应为 HH:MM，got %q", window.End)
		}
		if st >= en {
			return nil, errors.New("peak window end 必须大于 start（不支持跨天区间，如 22:00-02:00）")
		}
		if window.Multiplier < 0 {
			return nil, errors.New("peak window multiplier 不能为负")
		}
		window.Start = canonicalStart
		window.End = canonicalEnd
		normalized = append(normalized, window)
	}

	sort.SliceStable(normalized, func(i, j int) bool {
		left, _ := parseMinutes(normalized[i].Start)
		right, _ := parseMinutes(normalized[j].Start)
		return left < right
	})
	for i := 1; i < len(normalized); i++ {
		prevEnd, _ := parseMinutes(normalized[i-1].End)
		curStart, _ := parseMinutes(normalized[i].Start)
		if curStart < prevEnd {
			return nil, errors.New("peak_rate_windows 不允许重叠")
		}
	}
	return normalized, nil
}

func PeakRateLegacyFields(windows []PeakRateWindow) (start, end string, multiplier float64) {
	if len(windows) == 0 {
		return "", "", 1.0
	}
	first := windows[0]
	return first.Start, first.End, first.Multiplier
}

func PeakRateWindowsForStorage(windows []PeakRateWindow) []PeakRateWindow {
	if len(windows) == 0 {
		return []PeakRateWindow{}
	}
	return windows
}

func PeakRateWindowsForRead(windows []PeakRateWindow, start, end string, multiplier float64) []PeakRateWindow {
	return PeakRateWindowsForStorage(normalizedPeakRateWindowsForRead(windows, start, end, multiplier))
}

func peakMultiplierForAPIKey(apiKey *APIKey, now time.Time) float64 {
	if apiKey == nil || apiKey.Group == nil {
		return 1.0
	}
	return apiKey.Group.PeakMultiplierAt(now)
}

func singlePeakRateWindowFromLegacy(start, end string, multiplier float64) []PeakRateWindow {
	if strings.TrimSpace(start) == "" && strings.TrimSpace(end) == "" {
		return nil
	}
	return []PeakRateWindow{{
		Start:      start,
		End:        end,
		Multiplier: multiplier,
	}}
}

func normalizedPeakRateWindowsForRead(windows []PeakRateWindow, start, end string, multiplier float64) []PeakRateWindow {
	legacy, legacyErr := normalizePeakRateWindowsForRead(singlePeakRateWindowFromLegacy(start, end, multiplier))
	if len(windows) > 0 {
		normalized, err := normalizePeakRateWindowsForRead(windows)
		if err == nil {
			if len(legacy) > 0 && !samePeakRateWindow(normalized[0], legacy[0]) {
				merged := append([]PeakRateWindow{legacy[0]}, normalized[1:]...)
				if normalizedMerged, err := NormalizePeakRateWindows(true, merged); err == nil {
					return normalizedMerged
				}
				return legacy
			}
			return normalized
		}
	}
	if legacyErr != nil {
		return nil
	}
	return legacy
}

func normalizePeakRateWindowsForRead(windows []PeakRateWindow) ([]PeakRateWindow, error) {
	if len(windows) == 0 {
		return nil, errors.New("peak_rate_windows is empty")
	}
	normalized := make([]PeakRateWindow, 0, len(windows))
	for _, window := range windows {
		start, canonicalStart, okStart := parseLegacyMinutes(window.Start)
		if !okStart {
			return nil, fmt.Errorf("peak window start format should be HH:MM, got %q", window.Start)
		}
		end, canonicalEnd, okEnd := parseLegacyMinutes(window.End)
		if !okEnd {
			return nil, fmt.Errorf("peak window end format should be HH:MM, got %q", window.End)
		}
		if start >= end {
			return nil, errors.New("peak window end must be greater than start")
		}
		if window.Multiplier < 0 {
			return nil, errors.New("peak window multiplier cannot be negative")
		}
		window.Start = canonicalStart
		window.End = canonicalEnd
		normalized = append(normalized, window)
	}
	return NormalizePeakRateWindows(true, normalized)
}

func samePeakRateWindow(a, b PeakRateWindow) bool {
	return a.Start == b.Start && a.End == b.End && a.Multiplier == b.Multiplier
}

// computePeakAwareMultipliers 把"基础 token 倍率 base"（已含系统/分组/用户级倍率，但不含高峰）
// 拆分为最终 token 倍率与图片按次倍率：token 和图片按次都会在 base 上叠加高峰因子。
// gateway_service.recordUsageCore 与 openai_gateway_service.RecordUsage 共用此函数，
// 锁死"所有计费模式都吃高峰倍率"这一叠加顺序——任何调换都会被 group_peak_rate_test 覆盖。
func computePeakAwareMultipliers(apiKey *APIKey, base float64, now time.Time) (text, image float64) {
	peak := peakMultiplierForAPIKey(apiKey, now)
	text = base * peak
	image = resolveImageRateMultiplier(apiKey, base) * peak
	return
}

func computePeakAwareVideoMultiplier(apiKey *APIKey, base float64, now time.Time) float64 {
	return resolveVideoRateMultiplier(apiKey, base) * peakMultiplierForAPIKey(apiKey, now)
}
