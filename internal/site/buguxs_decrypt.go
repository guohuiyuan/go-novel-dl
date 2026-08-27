package site

import (
	_ "embed"
	"encoding/base64"
	"regexp"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

//go:embed resources/buguxs_get.js
var buguxsGetJS string

var buguxsVarCRe = regexp.MustCompile(`var c="([^"]+)"`)

// buguxsDecrypter 用 goja 执行站点的 php_decrypt_js 解密 var c 内容。
// get20260103.js 是 jsjiami 混淆脚本，主逻辑依赖浏览器环境，但
// php_decrypt_js 是纯函数（函数声明 hoisted），在 goja 里可直接调用。
type buguxsDecrypter struct {
	mu sync.Mutex
	vm *goja.Runtime
	fn goja.Callable
}

var buguxsDecryptOnce sync.Once
var buguxsDecryptInstance *buguxsDecrypter

func buguxsGetDecrypter() *buguxsDecrypter {
	buguxsDecryptOnce.Do(func() {
		buguxsDecryptInstance = newBuguxsDecrypter()
	})
	return buguxsDecryptInstance
}

func newBuguxsDecrypter() *buguxsDecrypter {
	vm := goja.New()
	vm.Set("atob", func(call goja.FunctionCall) goja.Value {
		s := call.Argument(0).String()
		d, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return vm.ToValue("")
		}
		var sb strings.Builder
		for _, b := range d {
			sb.WriteRune(rune(b))
		}
		return vm.ToValue(sb.String())
	})
	vm.Set("btoa", func(call goja.FunctionCall) goja.Value {
		s := call.Argument(0).String()
		raw := make([]byte, 0, len(s))
		for _, r := range s {
			raw = append(raw, byte(r))
		}
		return vm.ToValue(base64.StdEncoding.EncodeToString(raw))
	})
	vm.Set("console", map[string]interface{}{
		"log":   func(...interface{}) {},
		"warn":  func(...interface{}) {},
		"error": func(...interface{}) {},
	})
	vm.Set("window", vm.NewObject())
	vm.Set("document", vm.NewObject())
	vm.Set("navigator", vm.NewObject())
	vm.Set("location", map[string]interface{}{"href": ""})
	vm.Set("localStorage", map[string]interface{}{"getItem": func(interface{}) interface{} { return nil }, "setItem": func(...interface{}) {}, "removeItem": func(...interface{}) {}})
	vm.Set("sessionStorage", map[string]interface{}{"getItem": func(interface{}) interface{} { return nil }, "setItem": func(...interface{}) {}, "removeItem": func(...interface{}) {}})

	// 主逻辑会因缺少 DOM 抛错，但 php_decrypt_js 是函数声明，已 hoisted
	_, _ = vm.RunScript("buguxs_get.js", buguxsGetJS)

	var fn goja.Callable
	if v := vm.Get("php_decrypt_js"); v != nil {
		if callable, ok := goja.AssertFunction(v); ok {
			fn = callable
		}
	}
	return &buguxsDecrypter{vm: vm, fn: fn}
}

// Decrypt 解密 var c 内容，返回解密后的 HTML 文本。
func (d *buguxsDecrypter) Decrypt(c string) string {
	if d == nil || d.fn == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.fn(goja.Undefined(), d.vm.ToValue(strings.TrimSpace(c)))
	if err != nil {
		return ""
	}
	return res.String()
}

// buguxsExtractVarC 从章节页 HTML 提取 var c 的 Base64 内容。
func buguxsExtractVarC(markup string) string {
	if m := buguxsVarCRe.FindStringSubmatch(markup); len(m) == 2 {
		return m[1]
	}
	return ""
}
