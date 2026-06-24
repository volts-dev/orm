package orm

import (
	"reflect"
	"testing"
)

// TestParseM2MCommands 覆盖 many2many 命令元组的解析分类逻辑。
// 重点回归: 前端发来的精简二元组 [[4, id]] 必须被识别为命令,
// 否则会掉进旧版分支把整个 [4, id] 切片当成 id 传给 SQL,
// 触发 pq "invalid input syntax for type bigint" 错误。
func TestParseM2MCommands(t *testing.T) {
	cases := []struct {
		name     string
		input    []any
		wantOK   bool
		wantCmds [][]any
	}{
		{
			name:     "短二元组 Link (4,id) —— 前端实际写法",
			input:    []any{[]any{4, "2069486361299128320"}},
			wantOK:   true,
			wantCmds: [][]any{{4, "2069486361299128320"}},
		},
		{
			name:     "完整三元组 Link (4,id,0)",
			input:    []any{[]any{4, "100", 0}},
			wantOK:   true,
			wantCmds: [][]any{{4, "100", 0}},
		},
		{
			name:     "多个命令混合长短",
			input:    []any{[]any{3, "1"}, []any{4, "2", 0}},
			wantOK:   true,
			wantCmds: [][]any{{3, "1"}, {4, "2", 0}},
		},
		{
			name:     "Set 三元组 (6,0,ids)",
			input:    []any{[]any{6, 0, []any{"1", "2"}}},
			wantOK:   true,
			wantCmds: [][]any{{6, 0, []any{"1", "2"}}},
		},
		{
			name:   "普通 id 列表 —— 非命令, 交给旧逻辑",
			input:  []any{1, 2, 3},
			wantOK: false,
		},
		{
			name:   "空切片",
			input:  []any{},
			wantOK: false,
		},
		{
			name:   "非法 code (>6) 不视为命令",
			input:  []any{[]any{9, "1"}},
			wantOK: false,
		},
		{
			name:   "长度为1的元组不视为命令",
			input:  []any{[]any{4}},
			wantOK: false,
		},
		{
			name:   "命令与普通 id 混合 —— 整体判定为非命令",
			input:  []any{[]any{4, "1"}, 2},
			wantOK: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmds, ok := parseM2MCommands(c.input)
			if ok != c.wantOK {
				t.Fatalf("isAllCommands = %v, want %v", ok, c.wantOK)
			}
			if c.wantOK && !reflect.DeepEqual(cmds, c.wantCmds) {
				t.Fatalf("commands = %#v, want %#v", cmds, c.wantCmds)
			}
			if !ok && cmds != nil {
				t.Fatalf("非命令场景应返回 nil commands, got %#v", cmds)
			}
		})
	}
}
