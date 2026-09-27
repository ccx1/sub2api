package openaiauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Sentinel token 生成：移植自 customer_auth_security.py 的 SentinelSolver。
// 纯协议实现：sentinel/req -> PoW -> 组装 token；turnstile 的 t 字段由 TurnstileVM 计算。
//
// 说明：不引入任何第三方标识；sdk.js 版本串等易变量集中在此文件，便于上游变更时单点维护。
const (
	sentinelReqURL     = "https://sentinel.openai.com/backend-api/sentinel/req"
	sentinelReferer    = "https://sentinel.openai.com/backend-api/sentinel/frame.html"
	sentinelSDKURL     = "https://sentinel.openai.com/sentinel/20260124ceb8/sdk.js"
	sentinelMaxPoWIter = 500000
)

var sentinelScreens = []string{"1920x1080", "1536x864", "2560x1440", "1366x768", "1440x900", "1280x720"}
var sentinelHWConc = []int{4, 8, 12, 16}

// sentinelGeo 描述时区 / 语言上下文。
type sentinelGeo struct {
	TZ    string
	Lang  string
	Langs string
}

// SentinelSolver 保存单次登录会话的 sentinel 状态。
type SentinelSolver struct {
	deviceID  string
	userAgent string
	geo       sentinelGeo
	sid       string
	screen    string
	hwConc    int
}

func newSentinelSolver(deviceID, userAgent string, geo sentinelGeo) *SentinelSolver {
	if geo.TZ == "" {
		geo.TZ = "America/Los_Angeles"
	}
	if geo.Lang == "" {
		geo.Lang = "en-US"
	}
	if geo.Langs == "" {
		geo.Langs = "en-US,en"
	}
	return &SentinelSolver{
		deviceID:  deviceID,
		userAgent: userAgent,
		geo:       geo,
		sid:       randomUUID(),
		screen:    sentinelScreens[randInt(len(sentinelScreens))],
		hwConc:    sentinelHWConc[randInt(len(sentinelHWConc))],
	}
}

var tzLongName = map[string]string{
	"Asia/Tokyo":          "Japan Standard Time",
	"America/Los_Angeles": "Pacific Daylight Time",
	"America/New_York":    "Eastern Daylight Time",
	"America/Chicago":     "Central Daylight Time",
	"Europe/London":       "Greenwich Mean Time",
	"Europe/Berlin":       "Central European Summer Time",
	"Europe/Paris":        "Central European Summer Time",
	"Asia/Singapore":      "Singapore Standard Time",
	"Asia/Hong_Kong":      "Hong Kong Standard Time",
	"Asia/Seoul":          "Korean Standard Time",
}

// tzDateString 生成与时区一致的 JS Date.toString() 风格串。
func tzDateString(tzName string) string {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return time.Now().UTC().Format("Mon Jan 02 2006 15:04:05") + " GMT+0000 (Coordinated Universal Time)"
	}
	now := time.Now().In(loc)
	_, offsetSec := now.Zone()
	m := offsetSec / 60
	sign := "+"
	if m < 0 {
		sign = "-"
		m = -m
	}
	offStr := fmt.Sprintf("GMT%s%02d%02d", sign, m/60, m%60)
	longname := tzLongName[tzName]
	if longname == "" {
		longname = now.Format("MST")
	}
	return now.Format("Mon Jan 02 2006 15:04:05") + " " + offStr + " (" + longname + ")"
}

// config 生成 Python _get_config 对应的 26 元数组。
func (s *SentinelSolver) config() []any {
	navProps := []string{"vendorSub", "productSub", "vendor", "maxTouchPoints", "hardwareConcurrency",
		"cookieEnabled", "credentials", "mediaDevices", "permissions", "locks"}
	docProps := []string{"location", "implementation", "URL", "documentURI", "compatMode"}
	globalProps := []string{"Object", "Function", "Array", "Number", "parseFloat", "undefined"}
	perfNow := randFloat()*49000 + 1000
	timeOrigin := float64(time.Now().UnixNano())/1e6 - perfNow
	return []any{
		s.screen,
		tzDateString(s.geo.TZ),
		4294967296,
		randFloat(),
		s.userAgent,
		sentinelSDKURL,
		nil,
		nil,
		s.geo.Lang,
		s.geo.Langs,
		randFloat(),
		navProps[randInt(len(navProps))] + "\u2212undefined",
		docProps[randInt(len(docProps))],
		globalProps[randInt(len(globalProps))],
		perfNow,
		s.sid,
		"",
		s.hwConc,
		timeOrigin,
		0, 0, 0, 0, 0, 0, 0,
	}
}

func b64EncodeConfig(cfg []any) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func (s *SentinelSolver) generateRequirementsToken() (string, error) {
	cfg := s.config()
	cfg[3] = 1
	cfg[9] = float64(int(randFloat()*45 + 5))
	enc, err := b64EncodeConfig(cfg)
	if err != nil {
		return "", err
	}
	return "gAAAAAC" + enc, nil
}

// fnv1a32 与 customer_auth_security.py 的 _fnv1a_32 完全一致（含尾部混淆）。
func fnv1a32(text string) string {
	var h uint32 = 2166136261
	for _, ch := range text {
		h ^= uint32(ch)
		h = h * 16777619
	}
	h ^= h >> 16
	h = h * 2246822507
	h ^= h >> 13
	h = h * 3266489909
	h ^= h >> 16
	return fmt.Sprintf("%08x", h)
}

func (s *SentinelSolver) solvePoW(seed, difficulty string) (string, error) {
	start := time.Now()
	cfg := s.config()
	for nonce := 0; nonce < sentinelMaxPoWIter; nonce++ {
		cfg[3] = nonce
		cfg[9] = float64(time.Since(start).Milliseconds())
		enc, err := b64EncodeConfig(cfg)
		if err != nil {
			return "", err
		}
		digest := fnv1a32(seed + enc)
		if len(difficulty) <= len(digest) && digest[:len(difficulty)] <= difficulty {
			return "gAAAAAB" + enc + "~S", nil
		}
	}
	enc, _ := b64EncodeConfig([]any{nil})
	return "gAAAAAB" + enc, nil
}

// sentinelChallenge 是 sentinel/req 的响应结构。
type sentinelChallenge struct {
	Token       string `json:"token"`
	ProofOfWork struct {
		Required   bool   `json:"required"`
		Seed       string `json:"seed"`
		Difficulty string `json:"difficulty"`
	} `json:"proofofwork"`
	Turnstile struct {
		DX string `json:"dx"`
	} `json:"turnstile"`
}

// BuildToken 复刻 build_token：请求挑战、解 PoW、执行 turnstile VM、拼装最终 token。
// 由 transport 提供 post 能力，避免 openaiauth 与 http 层耦合。
func (s *SentinelSolver) BuildToken(post sentinelPoster, flow string) (string, error) {
	pToken, err := s.generateRequirementsToken()
	if err != nil {
		return "", err
	}
	reqBody, _ := json.Marshal(map[string]any{"p": pToken, "id": s.deviceID, "flow": flow})
	respRaw, err := post(sentinelReqURL, reqBody, map[string]string{
		"Content-Type": "text/plain;charset=UTF-8",
		"Accept":       "*/*",
		"Referer":      sentinelReferer,
		"Origin":       "https://sentinel.openai.com",
		"User-Agent":   s.userAgent,
	})
	if err != nil {
		return "", fmt.Errorf("sentinel/req: %w", err)
	}
	var challenge sentinelChallenge
	if err := json.Unmarshal(respRaw, &challenge); err != nil {
		return "", fmt.Errorf("sentinel/req decode: %w", err)
	}
	cValue := strings.TrimSpace(challenge.Token)
	if cValue == "" {
		return "", fmt.Errorf("sentinel challenge missing token")
	}
	var pValue string
	if challenge.ProofOfWork.Required && challenge.ProofOfWork.Seed != "" {
		diff := challenge.ProofOfWork.Difficulty
		if diff == "" {
			diff = "0"
		}
		if pValue, err = s.solvePoW(challenge.ProofOfWork.Seed, diff); err != nil {
			return "", err
		}
	} else {
		if pValue, err = s.generateRequirementsToken(); err != nil {
			return "", err
		}
	}
	var tValue any // null 时保持 JSON null
	if dx := challenge.Turnstile.DX; dx != "" {
		if t, err := runTurnstileVM(dx, pToken, s.userAgent, s.deviceID); err == nil && t != "" {
			tValue = t
		}
	}
	token, err := json.Marshal(map[string]any{
		"p": pValue, "t": tValue, "c": cValue, "id": s.deviceID, "flow": flow,
	})
	if err != nil {
		return "", err
	}
	return string(token), nil
}

// sentinelPoster 抽象一次 POST（由 transport 实现），返回响应体字节。
type sentinelPoster func(url string, body []byte, headers map[string]string) ([]byte, error)

// —— 随机数工具（crypto/rand 支撑，避免 math/rand 全局状态）——

func randInt(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

func randFloat() float64 {
	const prec = 1 << 53
	v, err := rand.Int(rand.Reader, big.NewInt(prec))
	if err != nil {
		return 0.5
	}
	return float64(v.Int64()) / float64(prec)
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
