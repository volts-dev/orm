package orm

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 日志格式串的守卫。
//
// ## 为什么要单独来一条
//
// `go vet` 的 printf 检查**默认认不出本仓的日志函数**：它只认 fmt 那一族，以及能被
// 它自动推断出来的包装。`log.Errf` / `Warnf` / `Panicf` 都不在其中，所以裸跑
// `go vet ./...` 是绿的，而下面这些一个都发现不了：
//
//	log.Errf("Non-stored field %s cannot be searched.", field.Name)  // 少括号 → 打出函数指针
//	log.Errf("asdf", err)                                            // 没有格式符 → err 被整个丢掉
//	log.Panicf(err.Error())                                          // 错误文本当格式串 → 带 % 就乱码
//
// 2026-08-20 一次性扫出 6 处，全是**诊断自己把要说的话弄丢了**：
//   - expr.go 那条是"非存储字段进了 domain"的唯一线索（后果是筛选条件被静默丢掉、
//     返回全表），却打成 `%!s(func() string=0xa84af00)`——在一次启动+升级的日志里出现
//     118 次，一个字段名都没点出来，等于装了灯没接线。
//   - 另外四处是 json.Marshal 失败后 `continue`，那一列就此**静默不写库**，而日志
//     只留下一句 "asdf"。
//
// ## 判据
//
// 用 `-printf.funcs` 把本仓的日志函数告诉 vet。参数只写函数名（不带包名），vet 会
// 匹配任意接收者上的同名方法。
func TestLogFormatDirectives(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		// 用 PATH 上的兜底；再找不到就跳过——守卫缺席好过测试因环境红。
		p, lerr := exec.LookPath("go")
		if lerr != nil {
			t.Skipf("找不到 go 工具链，跳过：%v", err)
		}
		goBin = p
	}

	cmd := exec.Command(goBin, "vet",
		"-printf.funcs=Errf,Warnf,Infof,Dbgf,Panicf,Fatalf",
		"./...")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil && text == "" {
		return
	}
	// 工具链本身跑不起来（版本不匹配 / GOFLAGS 之类）不该算失败。
	if strings.Contains(text, "go: ") && strings.Contains(text, "toolchain") {
		t.Skipf("go vet 跑不起来，跳过：\n%s", text)
	}
	t.Fatalf("日志格式串有问题（诊断会丢掉自己要说的内容）：\n%s\n\n"+
		"复现：go vet -printf.funcs=Errf,Warnf,Infof,Dbgf,Panicf,Fatalf ./...", text)
}
