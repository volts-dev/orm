package core

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
)

// 连接池的闸：等连接有上限、超了会报出名字。
//
// # 病灶
//
// database/sql 的池只有一个旋钮管"等连接"——调用方的 ctx。而 ctx 同时管着语句的
// 执行：QueryContext(ctx) 里"排队取连接"和"跑这条 SQL"是同一个 deadline。本仓的
// 会话 ctx 一律是 context.Background()，于是
//
//   - 没设 MaxOpenConns：永远不排队，连接数没有上限，全体进程加起来撞库的
//     max_connections；
//   - 设了 MaxOpenConns：池满时**无限期地等**。最常见的形状是一个请求握着事务的
//     连接、又去要第二条（Records()/NewSession() 各开一个会话），并发数一到上限
//     就互相等死，没有任何一行日志。
//
// 给会话 ctx 加 deadline 解决不了：那会把"等了多久"和"跑了多久"绑在一起，一条
// 合法的长语句会被排队时间挤掉；事务的 ctx 更是管到提交为止。
//
// # 处置
//
// 在池前面放一个**同样大小**的计数信号量。排队发生在这里而不是 database/sql 里，
// 于是能单独给它一个上限（AcquireTimeout），超了返回 ErrPoolTimeout——一个认得出
// 的错误，而不是一句 context deadline exceeded。语句真正交给 database/sql 时池里
// 必有空位，标准库自带的坏连接重试、预热、回收全部原样保留。
//
// # 槽位什么时候还
//
//   - Exec：语句返回。
//   - Query：Rows 关闭，或 Next() 读到头（标准库此时已把连接还回池）。
//   - 事务：Commit / Rollback 真正发出之后。
//
// 早还一拍无害（顶多让下一条语句在 database/sql 里短暂排队），晚还或不还才是泄漏，
// 所以每条路径都是"只还一次"的原子标志，重复调用是空操作。
//
// # 不经过闸的路径
//
// Ping、PrepareContext（Prepared 开关默认关、全仓没人开）、dialect.CreateDatabase
// 直接拿 *sql.DB。它们只在启动期出现，占一条连接的时间极短。
var ErrPoolTimeout = ormerr.ErrPoolTimeout

// PoolTimeoutError 带上现场：等了多久、池多大。errors.Is(err, ErrPoolTimeout) 成立。
type PoolTimeoutError struct {
	Waited  time.Duration
	MaxOpen int
}

func (e *PoolTimeoutError) Error() string {
	return fmt.Sprintf("%s (waited %s, max_open_conns=%d)",
		ErrPoolTimeout.Error(), e.Waited.Round(time.Millisecond), e.MaxOpen)
}

func (e *PoolTimeoutError) Is(target error) bool { return target == ErrPoolTimeout }

// PoolStats 是 sql.DBStats 加上闸自己的计数。
//
// 设了上限之后排队发生在闸上，sql.DBStats 的 WaitCount/WaitDuration 基本恒为 0，
// 调上限要看的是这里的三个数。
type PoolStats struct {
	sql.DBStats

	// GateWaitCount 是没能立刻拿到槽位、进了排队的次数。
	GateWaitCount int64
	// GateWaitDuration 是排队的累计时长（含最终超时的那些）。
	GateWaitDuration time.Duration
	// GateTimeoutCount 是排队超过 AcquireTimeout 而失败的次数。
	GateTimeoutCount int64
	// GatePeakInUse 是同时被占用的槽位数的历史峰值——上限该定多大就看它。
	GatePeakInUse int
	// AcquireTimeout 是当前的排队上限；0 表示只受调用方 ctx 约束。
	AcquireTimeout time.Duration
}

type poolGate struct {
	slots chan struct{}

	waitCount    atomic.Int64
	waitNanos    atomic.Int64
	timeoutCount atomic.Int64
	peak         atomic.Int64
}

func newPoolGate(n int) *poolGate {
	return &poolGate{slots: make(chan struct{}, n)}
}

func (g *poolGate) notePeak() {
	n := int64(len(g.slots))
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			return
		}
	}
}

func (g *poolGate) put() { <-g.slots }

// poolSlot 是一次占用。零值（gate 为 nil）表示没有闸，release 是空操作。
type poolSlot struct {
	gate     *poolGate
	released atomic.Bool
}

func (s *poolSlot) release() {
	if s == nil || s.gate == nil {
		return
	}
	if s.released.CompareAndSwap(false, true) {
		s.gate.put()
	}
}

// SetMaxOpenConns 设置池的上限，并把闸换成同样大小。n <= 0 表示不设上限（也就没有闸）。
//
// 盖住了 *sql.DB 上的同名方法：只调标准库那个的话，池有上限而闸没有，等连接又回到
// "无限期地等"。
//
// 运行中改上限是安全的：已经占着旧闸槽位的语句各自还回旧闸，新语句走新闸；
// 交接的那一小段里并发可能短暂超过新上限，多出来的由 database/sql 自己排队。
func (db *DB) SetMaxOpenConns(n int) {
	db.DB.SetMaxOpenConns(n)
	if n <= 0 {
		db.gate.Store(nil)
		return
	}
	db.gate.Store(newPoolGate(n))
}

// SetAcquireTimeout 设置等连接的上限。d <= 0 表示不单独设限（只受调用方 ctx 约束）。
// 没设 MaxOpenConns 时它不起作用——那时根本不排队。
func (db *DB) SetAcquireTimeout(d time.Duration) {
	if d < 0 {
		d = 0
	}
	db.acquireTimeout.Store(int64(d))
}

// PoolStats 返回池与闸的计数。
func (db *DB) PoolStats() PoolStats {
	st := PoolStats{
		DBStats:        db.DB.Stats(),
		AcquireTimeout: time.Duration(db.acquireTimeout.Load()),
	}
	if g := db.gate.Load(); g != nil {
		st.GateWaitCount = g.waitCount.Load()
		st.GateWaitDuration = time.Duration(g.waitNanos.Load())
		st.GateTimeoutCount = g.timeoutCount.Load()
		st.GatePeakInUse = int(g.peak.Load())
	}
	return st
}

// acquire 占一个槽位。没有闸时立刻返回 nil gate（release 是空操作）。
func (db *DB) acquire(ctx context.Context) (*poolGate, error) {
	g := db.gate.Load()
	if g == nil {
		return nil, nil
	}

	// 快路径：有空位，不碰计时器。
	select {
	case g.slots <- struct{}{}:
		g.notePeak()
		return g, nil
	default:
	}

	start := time.Now()
	g.waitCount.Add(1)

	var timeout <-chan time.Time
	if d := time.Duration(db.acquireTimeout.Load()); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		timeout = t.C
	}

	select {
	case g.slots <- struct{}{}:
		g.waitNanos.Add(int64(time.Since(start)))
		g.notePeak()
		return g, nil
	case <-timeout:
		waited := time.Since(start)
		g.waitNanos.Add(int64(waited))
		g.timeoutCount.Add(1)
		// 这一行是"池满了"在日志里唯一的痕迹：调用方拿到的错误多半被上层换成一句
		// 通用文案，而要调上限的人需要知道是哪个进程、等了多久、池多大。
		log.Warnf("connection pool exhausted: waited %s for a free connection (max_open_conns=%d, timeouts so far %d)",
			waited.Round(time.Millisecond), cap(g.slots), g.timeoutCount.Load())
		return nil, &PoolTimeoutError{Waited: waited, MaxOpen: cap(g.slots)}
	case <-ctx.Done():
		g.waitNanos.Add(int64(time.Since(start)))
		return nil, ctx.Err()
	}
}
