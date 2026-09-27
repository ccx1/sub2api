package openaiauth

import (
	"encoding/json"
	"strings"
	"time"
)

// 宿主对象建模：为 turnstile VM 提供 navigator / document / screen / performance /
// location 以及 JSON / Math 等全局。对齐 customer_auth_vm.js 里的 mock* 常量。
//
// 采用 map[string]any 表达对象，方法用 jsFunc 表达。VM 通过 opcode 6/24 访问
// 属性与方法；未建模的属性返回 nil，对应分支自然短路（与 JS undefined 行为接近）。

type jsObject map[string]any

func (vm *turnstileVM) initGlobals(userAgent, deviceID string) {
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.6943.88 Safari/537.36"
	}
	navigator := jsObject{
		"userAgent":           userAgent,
		"language":            "en-US",
		"languages":           []any{"en-US", "en"},
		"platform":            "Win32",
		"vendor":              "Google Inc.",
		"vendorSub":           "",
		"productSub":          "20030107",
		"hardwareConcurrency": 8,
		"maxTouchPoints":      0,
		"cookieEnabled":       true,
		"onLine":              true,
		"appCodeName":         "Mozilla",
		"appName":             "Netscape",
		"pdfViewerEnabled":    true,
		"webdriver":           false,
		"deviceMemory":        8,
		"connection":          jsObject{"effectiveType": "4g", "rtt": 50, "downlink": 10},
		"plugins":             jsObject{"length": 5},
		"mimeTypes":           jsObject{"length": 2},
	}
	perfOrigin := float64(time.Now().UnixNano())/1e6 - randFloat()*30000
	performance := jsObject{
		"now":        jsFunc(func(a ...any) any { return float64(time.Now().UnixNano())/1e6 - perfOrigin }),
		"timeOrigin": perfOrigin,
		"memory":     jsObject{"jsHeapSizeLimit": 4294705152, "totalJSHeapSize": 35000000, "usedJSHeapSize": 25000000},
	}
	screen := jsObject{
		"width": 1920, "height": 1080, "availWidth": 1920, "availHeight": 1040,
		"colorDepth": 24, "pixelDepth": 24,
	}
	document := jsObject{
		"scripts":         []any{jsObject{"src": sentinelSDKURL}},
		"documentElement": jsObject{"getAttribute": jsFunc(func(a ...any) any { return nil })},
		"body":            jsObject{"clientWidth": 1920, "clientHeight": 1080},
		"cookie":          "oai-did=" + deviceID,
		"URL":             "https://auth.openai.com/log-in",
		"referrer":        "https://chatgpt.com/",
		"title":           "Log in | OpenAI",
		"readyState":      "complete",
		"hidden":          false,
		"visibilityState": "visible",
	}
	location := jsObject{
		"href": "https://auth.openai.com/log-in", "origin": "https://auth.openai.com",
		"protocol": "https:", "host": "auth.openai.com", "hostname": "auth.openai.com",
		"pathname": "/log-in", "search": "", "hash": "",
	}
	location["toString"] = jsFunc(func(a ...any) any { return location["href"] })

	global := jsObject{
		"navigator": navigator, "screen": screen, "document": document,
		"performance": performance, "location": location,
		"localStorage": jsObject{"length": 0}, "sessionStorage": jsObject{"length": 0},
		"history": jsObject{"length": 2}, "chrome": jsObject{"runtime": jsObject{}},
		"innerWidth": 1920, "innerHeight": 1080, "outerWidth": 1920, "outerHeight": 1080,
		"devicePixelRatio": 1, "isSecureContext": true, "crossOriginIsolated": false,
		"JSON": jsObject{
			"parse": jsFunc(func(a ...any) any {
				var v any
				if jsonUnmarshalLoose(toStr(arg(a, 0)), &v) != nil {
					return nil
				}
				return v
			}),
			"stringify": jsFunc(func(a ...any) any { return jsonMarshalStr(arg(a, 0)) }),
		},
		"Math": jsObject{
			"random": jsFunc(func(a ...any) any { return randFloat() }),
			"floor":  jsFunc(func(a ...any) any { f, _ := toFloat(arg(a, 0)); return float64(int64(f)) }),
			"abs": jsFunc(func(a ...any) any {
				f, _ := toFloat(arg(a, 0))
				if f < 0 {
					return -f
				}
				return f
			}),
		},
		"Date": jsObject{
			"now": jsFunc(func(a ...any) any { return float64(time.Now().UnixMilli()) }),
		},
	}
	global["window"] = global
	global["self"] = global
	global["globalThis"] = global
	vm.globalObj = global
}

// jsonUnmarshalLoose / jsonMarshalStr 复用 encoding/json，但集中封装便于替换。
func jsonUnmarshalLoose(s string, v *any) error { return json.Unmarshal([]byte(s), v) }
func jsonMarshalStr(v any) string               { b, _ := json.Marshal(v); return string(b) }

// propGet 读取宿主对象属性；支持 jsObject、字符串（length / 索引方法）等。
func propGet(obj any, key any) any {
	if obj == nil {
		return nil
	}
	k := toStr(key)
	switch o := obj.(type) {
	case jsObject:
		return o[k]
	case map[string]any:
		return o[k]
	case []any:
		if k == "length" {
			return float64(len(o))
		}
		if idx, ok := toInt(key); ok && idx >= 0 && idx < len(o) {
			return o[idx]
		}
		return nil
	case string:
		if k == "length" {
			return float64(len(o))
		}
		if k == "charCodeAt" {
			s := o
			return jsFunc(func(a ...any) any {
				idx, _ := toInt(arg(a, 0))
				if idx >= 0 && idx < len(s) {
					return float64(s[idx])
				}
				return nil
			})
		}
		if k == "toString" {
			s := o
			return jsFunc(func(a ...any) any { return s })
		}
		if strings.HasPrefix(k, "") {
			if idx, ok := toInt(key); ok && idx >= 0 && idx < len(o) {
				return string(o[idx])
			}
		}
		return nil
	}
	return nil
}
