package orm

import "testing"

// 节点号要真的进到 id 里：id 的第 12–21 位就是节点号。两个进程节点号相同、同一毫秒各建第一条
// 记录，拿到的就是同一个 id。
func TestSetIdNodeChangesIdNodeBits(t *testing.T) {
	defer SetIdNode(1)
	for _, n := range []int64{3, 1023} {
		if err := SetIdNode(n); err != nil {
			t.Fatal(err)
		}
		if got := (NewId() >> 12) & 0x3ff; got != n {
			t.Fatalf("SetIdNode(%d) 之后 id 的节点位是 %d", n, got)
		}
	}
	if err := SetIdNode(1024); err == nil {
		t.Fatal("节点号超出 10 位应当报错")
	}
	if got := (NewId() >> 12) & 0x3ff; got != 1023 {
		t.Fatalf("非法节点号不该换掉现有的生成器，节点位成了 %d", got)
	}
}
