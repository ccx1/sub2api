package openaiauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Turnstile VM：移植自 customer-recovery-source/customer_auth_vm.js。
//
// 该文件在进程内解释执行 OpenAI 下发的 turnstile SDK 字节码（opcode 0~34），
// 产出 sentinel token 的 t 字段。完全用 Go 实现，不依赖 Node。
//
// 关键约定：VM 里的“字符串”按 JS latin1 语义处理——即把字符串视为字节序列，
// charCodeAt(i) 取第 i 个字节。base64 解码结果与 XOR 结果都落在 0~255，
// 解密出的 JSON 又是 ASCII，因此用 Go 的 []byte / string(字节) 表达可保持逐字节一致。
//
// host 对象（navigator/document/screen/... 与 JSON/Math 等）以 Go map / 闭包近似。
// 若上游程序访问了未建模的宿主属性，取值为 nil、相关分支自然跳过；
// 拿不到 t 时返回空串，调用方按 t=null 处理（与 Python 版一致）。

const turnstileMaxSteps = 200000

// jsFunc 是 VM 内可调用值：接收已解引用的实参，返回结果。
type jsFunc func(args ...any) any

type turnstileVM struct {
	regs      map[int]any
	steps     int
	resolved  bool
	result    string
	globalObj jsObject
}

// regsObj 把寄存器表包装为宿主对象，供 opcode 12（self ref）返回。
// 提供 get/set 方法，模拟 JS Map 接口的最小子集。
func (vm *turnstileVM) regsObj() jsObject {
	return jsObject{
		"get": jsFunc(func(a ...any) any { return vm.get(arg(a, 0)) }),
		"set": jsFunc(func(a ...any) any { vm.set(arg(a, 0), arg(a, 1)); return nil }),
	}
}

// runTurnstileVM 解密并执行 dx 程序，返回 base64 编码的 t 值（失败返回空串）。
func runTurnstileVM(dxB64, reqToken, userAgent, deviceID string) (t string, err error) {
	defer func() {
		if r := recover(); r != nil {
			t, err = "", fmt.Errorf("turnstile vm panic: %v", r)
		}
	}()
	vm := &turnstileVM{regs: make(map[int]any)}
	vm.initGlobals(userAgent, deviceID)
	vm.initOps(reqToken)

	// resolve / reject 句柄。
	vm.regs[3] = jsFunc(func(a ...any) any {
		if !vm.resolved {
			vm.resolved = true
			vm.result = base64.StdEncoding.EncodeToString([]byte(toStr(arg(a, 0))))
		}
		return nil
	})
	vm.regs[4] = jsFunc(func(a ...any) any {
		if !vm.resolved {
			vm.resolved = true
			vm.result = base64.StdEncoding.EncodeToString([]byte("ERR:" + toStr(arg(a, 0))))
		}
		return nil
	})

	raw, decErr := base64.StdEncoding.DecodeString(dxB64)
	if decErr != nil {
		return "", decErr
	}
	decrypted := xorDecrypt(string(raw), reqToken)
	var instructions []any
	if err := json.Unmarshal([]byte(decrypted), &instructions); err != nil {
		return "", fmt.Errorf("turnstile dx decode: %w", err)
	}
	vm.regs[9] = instructions
	vm.runQueue()
	return vm.result, nil
}

// xorDecrypt 复刻 SDK 的 Tt：逐字节 text[i] ^ key[i % len]。
func xorDecrypt(text, key string) string {
	if len(key) == 0 {
		return text
	}
	out := make([]byte, len(text))
	for i := 0; i < len(text); i++ {
		out[i] = text[i] ^ key[i%len(key)]
	}
	return string(out)
}

func (vm *turnstileVM) get(reg any) any {
	k, ok := toInt(reg)
	if !ok {
		return nil
	}
	return vm.regs[k]
}

func (vm *turnstileVM) set(reg any, val any) {
	if k, ok := toInt(reg); ok {
		vm.regs[k] = val
	}
}

// runQueue 执行寄存器 9 中的指令队列，直到 resolve 或队列耗尽。
func (vm *turnstileVM) runQueue() {
	for !vm.resolved {
		queue, ok := vm.regs[9].([]any)
		if !ok || len(queue) == 0 {
			break
		}
		instr, ok := queue[0].([]any)
		vm.regs[9] = queue[1:]
		if ok && len(instr) > 0 {
			handler, isFunc := vm.get(instr[0]).(jsFunc)
			if isFunc {
				func() {
					defer func() { _ = recover() }()
					handler(instr[1:]...)
				}()
			}
		}
		vm.steps++
		if vm.steps > turnstileMaxSteps {
			break
		}
	}
}

// initOps 注册 opcode 0~34（对齐 customer_auth_vm.js 的 initVM）。
func (vm *turnstileVM) initOps(reqToken string) {
	r := vm.regs
	r[16] = reqToken
	r[10] = vm.globalObj

	// 0: 执行内嵌加密程序
	r[0] = jsFunc(func(a ...any) any {
		dxVal := toStr(vm.get(arg(a, 0)))
		if dxVal == "" {
			return nil
		}
		raw, err := base64.StdEncoding.DecodeString(dxVal)
		if err != nil {
			return nil
		}
		decrypted := xorDecrypt(string(raw), toStr(vm.get(16)))
		var nested []any
		if json.Unmarshal([]byte(decrypted), &nested) != nil {
			return nil
		}
		saved, _ := vm.regs[9].([]any)
		vm.regs[9] = append(append([]any{}, nested...), saved...)
		return nil
	})
	// 1: XOR
	r[1] = jsFunc(func(a ...any) any {
		vm.set(arg(a, 0), xorDecrypt(toStr(vm.get(arg(a, 0))), toStr(vm.get(arg(a, 1)))))
		return nil
	})
	// 2: SET（字面量）
	r[2] = jsFunc(func(a ...any) any { vm.set(arg(a, 0), arg(a, 1)); return nil })
	// 5: PUSH / CONCAT
	r[5] = jsFunc(func(a ...any) any {
		o := vm.get(arg(a, 0))
		v := vm.get(arg(a, 1))
		if arr, ok := o.([]any); ok {
			vm.set(arg(a, 0), append(arr, v))
		} else {
			vm.set(arg(a, 0), toStr(o)+toStr(v))
		}
		return nil
	})
	// 6: PROP ACCESS
	r[6] = jsFunc(func(a ...any) any {
		vm.set(arg(a, 0), propGet(vm.get(arg(a, 1)), vm.get(arg(a, 2))))
		return nil
	})
	// 7: CALL VOID
	r[7] = jsFunc(func(a ...any) any {
		if fn, ok := vm.get(arg(a, 0)).(jsFunc); ok {
			fn(vm.deref(a[1:])...)
		}
		return nil
	})
	// 8: COPY
	r[8] = jsFunc(func(a ...any) any { vm.set(arg(a, 0), vm.get(arg(a, 1))); return nil })
	// 11: SCRIPT MATCH（简化：返回 sdk.js 脚本地址）
	r[11] = jsFunc(func(a ...any) any {
		vm.set(arg(a, 0), sentinelSDKURL)
		return nil
	})
	// 12: SELF REF（返回寄存器表本身，用宿主对象近似）
	r[12] = jsFunc(func(a ...any) any { vm.set(arg(a, 0), vm.regsObj()); return nil })
	// 13: TRY CALL
	r[13] = jsFunc(func(a ...any) any {
		defer func() {
			if rec := recover(); rec != nil {
				vm.set(arg(a, 0), fmt.Sprintf("%v", rec))
			}
		}()
		if fn, ok := vm.get(arg(a, 1)).(jsFunc); ok {
			fn(a[2:]...)
		}
		return nil
	})
	// 14: JSON.parse
	r[14] = jsFunc(func(a ...any) any {
		var v any
		if json.Unmarshal([]byte(toStr(vm.get(arg(a, 1)))), &v) != nil {
			vm.set(arg(a, 0), nil)
		} else {
			vm.set(arg(a, 0), v)
		}
		return nil
	})
	// 15: JSON.stringify
	r[15] = jsFunc(func(a ...any) any {
		b, _ := json.Marshal(vm.get(arg(a, 1)))
		vm.set(arg(a, 0), string(b))
		return nil
	})
	// 17: CALL with return
	r[17] = jsFunc(func(a ...any) any {
		defer func() {
			if rec := recover(); rec != nil {
				vm.set(arg(a, 0), fmt.Sprintf("%v", rec))
			}
		}()
		if fn, ok := vm.get(arg(a, 1)).(jsFunc); ok {
			vm.set(arg(a, 0), fn(vm.deref(a[2:])...))
		}
		return nil
	})
	// 18: atob
	r[18] = jsFunc(func(a ...any) any {
		raw, _ := base64.StdEncoding.DecodeString(toStr(vm.get(arg(a, 0))))
		vm.set(arg(a, 0), string(raw))
		return nil
	})
	// 19: btoa
	r[19] = jsFunc(func(a ...any) any {
		vm.set(arg(a, 0), base64.StdEncoding.EncodeToString([]byte(toStr(vm.get(arg(a, 0))))))
		return nil
	})
	// 20: IF EQUAL
	r[20] = jsFunc(func(a ...any) any {
		if looseEq(vm.get(arg(a, 0)), vm.get(arg(a, 1))) {
			if fn, ok := vm.get(arg(a, 2)).(jsFunc); ok {
				fn(a[3:]...)
			}
		}
		return nil
	})
	// 21: IF DIFF > threshold
	r[21] = jsFunc(func(a ...any) any {
		n, _ := toFloat(vm.get(arg(a, 0)))
		e, _ := toFloat(vm.get(arg(a, 1)))
		thr, _ := toFloat(vm.get(arg(a, 2)))
		if math.Abs(n-e) > thr {
			if fn, ok := vm.get(arg(a, 3)).(jsFunc); ok {
				fn(a[4:]...)
			}
		}
		return nil
	})
	// 22: EXEC SUB
	r[22] = jsFunc(func(a ...any) any {
		saved, _ := vm.regs[9].([]any)
		vm.regs[9] = append([]any{}, a[1:]...)
		vm.runQueue()
		vm.set(arg(a, 0), strconv.Itoa(vm.steps))
		vm.regs[9] = saved
		return nil
	})
	// 23: IF DEFINED
	r[23] = jsFunc(func(a ...any) any {
		if vm.get(arg(a, 0)) != nil {
			if fn, ok := vm.get(arg(a, 1)).(jsFunc); ok {
				fn(a[2:]...)
			}
		}
		return nil
	})
	// 24: BIND
	r[24] = jsFunc(func(a ...any) any {
		obj := vm.get(arg(a, 1))
		method := propGet(obj, vm.get(arg(a, 2)))
		if fn, ok := method.(jsFunc); ok {
			vm.set(arg(a, 0), fn)
		} else {
			vm.set(arg(a, 0), nil)
		}
		return nil
	})
	// 25/26/28: NOP
	r[25] = jsFunc(func(a ...any) any { return nil })
	r[26] = jsFunc(func(a ...any) any { return nil })
	r[28] = jsFunc(func(a ...any) any { return nil })
	// 27: REMOVE / SUBTRACT
	r[27] = jsFunc(func(a ...any) any {
		o := vm.get(arg(a, 0))
		if arr, ok := o.([]any); ok {
			target := vm.get(arg(a, 1))
			for i, v := range arr {
				if looseEq(v, target) {
					vm.set(arg(a, 0), append(append([]any{}, arr[:i]...), arr[i+1:]...))
					break
				}
			}
		} else {
			n, _ := toFloat(o)
			e, _ := toFloat(vm.get(arg(a, 1)))
			vm.set(arg(a, 0), n-e)
		}
		return nil
	})
	// 29: LESS THAN
	r[29] = jsFunc(func(a ...any) any {
		e, _ := toFloat(vm.get(arg(a, 1)))
		rr, _ := toFloat(vm.get(arg(a, 2)))
		vm.set(arg(a, 0), e < rr)
		return nil
	})
	// 30: DEF FUNC
	r[30] = jsFunc(func(a ...any) any {
		target := arg(a, 0)
		retReg := arg(a, 1)
		third := arg(a, 2)
		fourth := arg(a, 3)
		params, isArr := third.([]any)
		var body []any
		if isArr {
			body, _ = fourth.([]any)
		} else {
			body, _ = third.([]any)
			params = nil
		}
		vm.set(target, jsFunc(func(callArgs ...any) any {
			if vm.resolved {
				return nil
			}
			saved, _ := vm.regs[9].([]any)
			if isArr {
				for i := 0; i < len(params); i++ {
					if i < len(callArgs) {
						vm.set(params[i], callArgs[i])
					} else {
						vm.set(params[i], nil)
					}
				}
			}
			vm.regs[9] = append([]any{}, body...)
			vm.runQueue()
			result := vm.get(retReg)
			vm.regs[9] = saved
			return result
		}))
		return nil
	})
	// 33: MULTIPLY
	r[33] = jsFunc(func(a ...any) any {
		e, _ := toFloat(vm.get(arg(a, 1)))
		rr, _ := toFloat(vm.get(arg(a, 2)))
		vm.set(arg(a, 0), e*rr)
		return nil
	})
	// 34: AWAIT（同步近似）
	r[34] = jsFunc(func(a ...any) any { vm.set(arg(a, 0), vm.get(arg(a, 1))); return nil })
}

// deref 把指令实参列表逐个 vt.get。
func (vm *turnstileVM) deref(args []any) []any {
	out := make([]any, len(args))
	for i, v := range args {
		out[i] = vm.get(v)
	}
	return out
}

func arg(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

// —— 值转换工具（JS 松散语义近似）——

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return int(f), true
		}
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func toStr(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case bool:
		if s {
			return "true"
		}
		return "false"
	case float64:
		if s == math.Trunc(s) && !math.IsInf(s, 0) {
			return strconv.FormatInt(int64(s), 10)
		}
		return strconv.FormatFloat(s, 'g', -1, 64)
	case int:
		return strconv.Itoa(s)
	case json.Number:
		return s.String()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func looseEq(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
	}
	return toStr(a) == toStr(b)
}
