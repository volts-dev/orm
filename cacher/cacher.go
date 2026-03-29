package cacher

import (
	"fmt"
	"sync"
	"time"

	"github.com/volts-dev/cacher"
	_ "github.com/volts-dev/cacher/memory"
	"github.com/volts-dev/dataset"
	"github.com/volts-dev/volts/logger"
)

var log = logger.New("orm")

// 缓存容量和TTL限制
const (
	// DefaultMaxCacheSize 缓存索引单表最大数量，防止无限增长导致OOM
	DefaultMaxCacheSize = 10000
	// DefaultCacheTTL 默认缓存过期时间（秒）
	DefaultCacheTTL = 3600 // 1小时
	// MinCacheTTL 最小缓存过期时间（秒）
	MinCacheTTL = 60 // 1分钟
	// MaxCacheTTL 最大缓存过期时间（秒）
	MaxCacheTTL = 86400 // 24小时
)

// CacheEntry 代表单个缓存条目，包含过期时间
type CacheEntry struct {
	Value      interface{}
	ExpiryTime int64 // Unix时间戳，秒级
}

// TODO cache name
// TODO 9缓存必须是支持读写多个ORM共享
type (
	// FIXME 未提供使用
	//ModelCacher interface {
	//	PutById(table string, id interface{}, record *dataset.TRecordSet)
	//	GetBySql(table string, sql string, arg interface{}) *dataset.TDataSet
	//}

	TCacher struct {
		sync.RWMutex
		active                   bool
		interval                 int
		expired                  int
		ttl                      int64 // 缓存过期时间（秒）
		lastCleanupTime          int64 // 上次清理过期缓存的时间戳
		status                   map[string]bool
		id_caches                cacher.ICacher // 缓存Id 对应记录 map[model]record
		sql_caches               cacher.ICacher // 缓存Sql查询结果
		table_id_key_index       map[string]map[string]bool
		table_sql_key_index      map[string]map[string]bool
		table_id_key_index_lock  sync.RWMutex
		table_sql_key_index_lock sync.RWMutex
		table_id_expiry          map[string]map[string]int64 // 记录Id缓存的过期时间
		table_sql_expiry         map[string]map[string]int64 // 记录Sql缓存的过期时间
		table_id_expiry_lock     sync.RWMutex
		table_sql_expiry_lock    sync.RWMutex
	}
)

func New() (*TCacher, error) {
	chr := &TCacher{
		status:              make(map[string]bool),
		table_id_key_index:  make(map[string]map[string]bool),
		table_sql_key_index: make(map[string]map[string]bool),
		table_id_expiry:     make(map[string]map[string]int64),
		table_sql_expiry:    make(map[string]map[string]int64),
		ttl:                 DefaultCacheTTL,
		lastCleanupTime:     time.Now().Unix(),
	}
	var err error

	chr.id_caches, err = cacher.New("memory")
	if err != nil {
		return nil, err
	}

	chr.sql_caches, err = cacher.New("memory")
	if err != nil {
		return nil, err
	}

	return chr, nil
}

// @removed 是否用于移除（内部版本，不持有锁）
func (self *TCacher) _genIdKeyUnsafe(table string, key interface{}, removed bool) string {
	str := fmt.Sprintf("%v-%v", table, key)

	var (
		tb  map[string]bool
		has bool
	)
	if tb, has = self.table_id_key_index[table]; !has {
		tb = make(map[string]bool)
		self.table_id_key_index[table] = tb
	}

	if removed {
		delete(tb, str)
		return str
	} else {
		// 防止缓存索引无限增长，超过容量时清理
		if len(tb) >= DefaultMaxCacheSize {
			log.Warn("table_id_key_index for table %s reached max size %d, clearing old entries",
				table, DefaultMaxCacheSize)
			// 清理缓存中的数据
			for k := range tb {
				self.id_caches.Delete(k)
				delete(tb, k)
			}
		}
		tb[str] = true
	}

	return str
}

// @removed 是否用于移除
func (self *TCacher) genIdKey(table string, key interface{}, removed bool) string {
	self.table_id_key_index_lock.Lock()
	defer self.table_id_key_index_lock.Unlock()
	return self._genIdKeyUnsafe(table, key, removed)
}

func (self *TCacher) genSqlKey(table string, sql string, args interface{}, removed bool) string {
	//# lock
	self.table_sql_key_index_lock.Lock()
	defer self.table_sql_key_index_lock.Unlock()
	return self._genSqlKeyUnsafe(table, sql, args, removed)
}

// @removed 是否用于移除（内部版本，不持有锁）
func (self *TCacher) _genSqlKeyUnsafe(table string, sql string, args interface{}, removed bool) string {
	str := fmt.Sprintf("%v-%v-%v", table, sql, args)
	// # 添加索引
	var (
		tb  map[string]bool
		has bool
	)

	if tb, has = self.table_sql_key_index[table]; !has {
		tb = make(map[string]bool)
		self.table_sql_key_index[table] = tb
	}

	// #移除索引
	if removed {
		delete(tb, str)
		return str
	} else {
		// 防止缓存索引无限增长，超过容量时清理
		if len(tb) >= DefaultMaxCacheSize {
			log.Warn("table_sql_key_index for table %s reached max size %d, clearing old entries",
				table, DefaultMaxCacheSize)
			// 清理缓存中的数据
			for k := range tb {
				self.sql_caches.Delete(k)
				delete(tb, k)
			}
		}
		tb[str] = true
	}

	return str
}

// turn on the cacher for query
func (self *TCacher) Active(sw bool) {
	self.active = sw
}

// SetTTL 设置缓存过期时间（秒），必须在MinCacheTTL和MaxCacheTTL之间
func (self *TCacher) SetTTL(ttl int64) {
	if ttl < MinCacheTTL {
		ttl = MinCacheTTL
		log.Warn("TTL too small, using minimum: %d seconds", MinCacheTTL)
	} else if ttl > MaxCacheTTL {
		ttl = MaxCacheTTL
		log.Warn("TTL too large, using maximum: %d seconds", MaxCacheTTL)
	}
	self.ttl = ttl
	log.Info("Cache TTL set to %d seconds", ttl)
}

// GetTTL 获取当前缓存过期时间
func (self *TCacher) GetTTL() int64 {
	return self.ttl
}

func (self *TCacher) SetExpired(expired int) {
	self.expired = expired
}

func (self *TCacher) SetInterval(interval int) {
	self.interval = interval
}

// set table of cacher status
func (self *TCacher) SetStatus(sw bool, table_name string) {
	self.status[table_name] = sw
}

// #缓存Sql查询结果ID集
func (self *TCacher) PutBySql(table string, sql string, arg interface{}, data *dataset.TDataSet) {
	if open, has := self.status[table]; has && open {
		key := self.genSqlKey(table, sql, arg, false)
		self.sql_caches.Set(&cacher.CacheBlock{Key: key, Value: data})

		// 记录过期时间
		expiryTime := time.Now().Unix() + self.ttl
		self.table_sql_expiry_lock.Lock()
		if _, has := self.table_sql_expiry[table]; !has {
			self.table_sql_expiry[table] = make(map[string]int64)
		}
		self.table_sql_expiry[table][key] = expiryTime
		self.table_sql_expiry_lock.Unlock()
	}
}

// #通过Sql获取查询结果ID集
// @Return:  nil or 空[]string
// WARNING: 返回的 *dataset.TDataSet 是缓存中的直接引用，请勿修改其内容，否则会污染缓存。
// 如需修改，请先复制一份副本。
func (self *TCacher) GetBySql(table string, sql string, arg interface{}) *dataset.TDataSet {
	if open, has := self.status[table]; has && open {
		key := self.genSqlKey(table, sql, arg, false)

		// 检查缓存是否已过期
		self.table_sql_expiry_lock.RLock()
		expiryTime, expired := self.table_sql_expiry[table][key]
		self.table_sql_expiry_lock.RUnlock()

		if expired && self.isExpired(expiryTime) {
			// 缓存已过期，删除并返回nil
			self.sql_caches.Delete(key)
			self.table_sql_expiry_lock.Lock()
			delete(self.table_sql_expiry[table], key)
			self.table_sql_expiry_lock.Unlock()
			return nil
		}

		v, err := self.sql_caches.Get(key)
		if err != nil {
			return nil
		}
		ds := v.(*dataset.TDataSet)
		log.Trace("Cache hit for table %s, key %s", table, key)
		return ds
	}

	return nil
}

// #缓存记录及ID
func (self *TCacher) PutById(table string, id interface{}, record *dataset.TRecordSet) {
	if open, has := self.status[table]; !has || (has && open) {
		//ck := self.RecCacher(table)
		key := self.genIdKey(table, id, false)
		self.id_caches.Set(&cacher.CacheBlock{Key: key, Value: record})

		// 记录过期时间
		expiryTime := time.Now().Unix() + self.ttl
		self.table_id_expiry_lock.Lock()
		if _, has := self.table_id_expiry[table]; !has {
			self.table_id_expiry[table] = make(map[string]int64)
		}
		self.table_id_expiry[table][key] = expiryTime
		self.table_id_expiry_lock.Unlock()
	}
}

// #通过ID获取记录
func (self *TCacher) GetByIds(table string, ids ...interface{}) (records []*dataset.TRecordSet, ids_less []interface{}) {
	if !self.active {
		return nil, ids
	}

	if open, has := self.status[table]; !has || (has && open) {
		for _, id := range ids {
			key := self.genIdKey(table, id, false)

			// 检查缓存是否已过期
			self.table_id_expiry_lock.RLock()
			expiryTime, expired := self.table_id_expiry[table][key]
			self.table_id_expiry_lock.RUnlock()

			var v interface{}
			var err error

			if expired && self.isExpired(expiryTime) {
				// 缓存已过期
				self.id_caches.Delete(key)
				self.table_id_expiry_lock.Lock()
				delete(self.table_id_expiry[table], key)
				self.table_id_expiry_lock.Unlock()
				ids_less = append(ids_less, id)
				continue
			}

			v, err = self.id_caches.Get(key)
			if err != nil {
				ids_less = append(ids_less, id)
				continue
			}
			records = append(records, v.(*dataset.TRecordSet))
		}

		return records, ids_less
	} else {
		return nil, ids
	}
}

func (self *TCacher) RemoveById(table string, ids ...interface{}) {
	self.table_id_key_index_lock.Lock()
	defer self.table_id_key_index_lock.Unlock()

	if _, has := self.table_id_key_index[table]; has {
		for _, id := range ids {
			key := self._genIdKeyUnsafe(table, id, true)

			self.id_caches.Delete(key)

			// 清理过期时间记录
			self.table_id_expiry_lock.Lock()
			if _, has := self.table_id_expiry[table]; has {
				delete(self.table_id_expiry[table], key)
			}
			self.table_id_expiry_lock.Unlock()
		}
	}
}

func (self *TCacher) RemoveBySql(table string, sqls ...string) {
	self.table_sql_key_index_lock.Lock()
	defer self.table_sql_key_index_lock.Unlock()
	if _, has := self.table_sql_key_index[table]; has {
		for _, sql := range sqls {
			key := self._genSqlKeyUnsafe(table, sql, "", true)

			self.sql_caches.Delete(key)

			// 清理过期时间记录
			self.table_sql_expiry_lock.Lock()
			if _, has := self.table_sql_expiry[table]; has {
				delete(self.table_sql_expiry[table], key)
			}
			self.table_sql_expiry_lock.Unlock()
		}
	}
}

func (self *TCacher) ClearByTable(table string) {
	self.table_id_key_index_lock.Lock()
	if m, has := self.table_id_key_index[table]; has {
		for key := range m {
			self.id_caches.Delete(key)
		}
		delete(self.table_id_key_index, table)
	}
	self.table_id_key_index_lock.Unlock()

	self.table_sql_key_index_lock.Lock()
	if m, has := self.table_sql_key_index[table]; has {
		for key := range m {
			self.sql_caches.Delete(key)
		}
		delete(self.table_sql_key_index, table)
	}
	self.table_sql_key_index_lock.Unlock()

	// 清理过期时间记录
	self.table_id_expiry_lock.Lock()
	delete(self.table_id_expiry, table)
	self.table_id_expiry_lock.Unlock()

	self.table_sql_expiry_lock.Lock()
	delete(self.table_sql_expiry, table)
	self.table_sql_expiry_lock.Unlock()
}

// CacheWarmer 缓存预热器，用于在启动时加载热数据到缓存
type CacheWarmer struct {
	cacher *TCacher
	tasks  []WarmupTask
}

// WarmupTask 代表一个缓存预热任务
type WarmupTask struct {
	Table    string
	SQL      string
	Args     []interface{}
	QueryFn  func(sql string, args ...interface{}) (*dataset.TDataSet, error) // 查询函数
	Priority int                                                              // 优先级（0-100，数字越大优先级越高）
}

// NewCacheWarmer 创建新的缓存预热器
func NewCacheWarmer(cacher *TCacher) *CacheWarmer {
	return &CacheWarmer{
		cacher: cacher,
		tasks:  make([]WarmupTask, 0),
	}
}

// AddTask 添加一个缓存预热任务
func (cw *CacheWarmer) AddTask(task WarmupTask) {
	if task.Priority < 0 {
		task.Priority = 0
	}
	if task.Priority > 100 {
		task.Priority = 100
	}
	cw.tasks = append(cw.tasks, task)
}

// AddHighPriorityTask 添加高优先级任务（优先级=80）
func (cw *CacheWarmer) AddHighPriorityTask(table, sql string, args []interface{}, queryFn func(sql string, args ...interface{}) (*dataset.TDataSet, error)) {
	cw.AddTask(WarmupTask{
		Table:    table,
		SQL:      sql,
		Args:     args,
		QueryFn:  queryFn,
		Priority: 80,
	})
}

// AddNormalTask 添加中优先级任务（优先级=50）
func (cw *CacheWarmer) AddNormalTask(table, sql string, args []interface{}, queryFn func(sql string, args ...interface{}) (*dataset.TDataSet, error)) {
	cw.AddTask(WarmupTask{
		Table:    table,
		SQL:      sql,
		Args:     args,
		QueryFn:  queryFn,
		Priority: 50,
	})
}

// Warm 执行缓存预热（阻塞操作）
func (cw *CacheWarmer) Warm() error {
	if len(cw.tasks) == 0 {
		log.Info("No warmup tasks to execute")
		return nil
	}

	// 按优先级排序（从高到低）
	for i := 0; i < len(cw.tasks); i++ {
		for j := i + 1; j < len(cw.tasks); j++ {
			if cw.tasks[j].Priority > cw.tasks[i].Priority {
				cw.tasks[i], cw.tasks[j] = cw.tasks[j], cw.tasks[i]
			}
		}
	}

	startTime := time.Now()
	successCount := 0
	failedCount := 0

	for idx, task := range cw.tasks {
		if task.QueryFn == nil {
			log.Warn("Task %d skipped: no query function provided", idx)
			failedCount++
			continue
		}

		result, err := task.QueryFn(task.SQL, task.Args...)
		if err != nil {
			log.Warn("Task %d failed for table '%s': %v", idx, task.Table, err)
			failedCount++
			continue
		}

		// 将结果存入缓存
		cw.cacher.PutBySql(task.Table, task.SQL, task.Args, result)
		successCount++

		log.Dbgf("Warmup task %d completed for table '%s'", idx, task.Table)
	}

	elapsed := time.Since(startTime)
	log.Info("Cache warmup completed: %d succeeded, %d failed, elapsed: %.2fs",
		successCount, failedCount, elapsed.Seconds())

	return nil
}

// WarmAsync 异步执行缓存预热
func (cw *CacheWarmer) WarmAsync() {
	go func() {
		if err := cw.Warm(); err != nil {
			log.Errf("Async warmup failed: %v", err)
		}
	}()
}

// WarmWithSchedule 按照给定的时间间隔定期执行缓存预热
func (cw *CacheWarmer) WarmWithSchedule(interval time.Duration) {
	if interval < 1*time.Minute {
		interval = 1 * time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if err := cw.Warm(); err != nil {
				log.Errf("Scheduled warmup failed: %v", err)
			}
		}
	}()

	log.Info("Started scheduled cache warmup with interval: %v", interval)
}

// isExpired 检查缓存是否已过期
func (self *TCacher) isExpired(expiryTime int64) bool {
	return time.Now().Unix() > expiryTime
}

// CleanupExpiredCache 清理所有已过期的缓存条目
// 该方法应该定期被调用（可以在独立的goroutine中）
func (self *TCacher) CleanupExpiredCache() {
	now := time.Now().Unix()

	// 检查是否需要清理（每分钟最多清理一次）
	if now-self.lastCleanupTime < 60 {
		return
	}

	self.lastCleanupTime = now
	expiredCount := 0

	// 清理Id缓存中的过期条目
	self.table_id_expiry_lock.Lock()
	for table := range self.table_id_expiry {
		for key, expiryTime := range self.table_id_expiry[table] {
			if now > expiryTime {
				self.id_caches.Delete(key)
				delete(self.table_id_expiry[table], key)
				expiredCount++
			}
		}
		// 如果表中没有剩余的过期时间记录，删除该表
		if len(self.table_id_expiry[table]) == 0 {
			delete(self.table_id_expiry, table)
		}
	}
	self.table_id_expiry_lock.Unlock()

	// 清理Sql缓存中的过期条目
	self.table_sql_expiry_lock.Lock()
	for table := range self.table_sql_expiry {
		for key, expiryTime := range self.table_sql_expiry[table] {
			if now > expiryTime {
				self.sql_caches.Delete(key)
				delete(self.table_sql_expiry[table], key)
				expiredCount++
			}
		}
		// 如果表中没有剩余的过期时间记录，删除该表
		if len(self.table_sql_expiry[table]) == 0 {
			delete(self.table_sql_expiry, table)
		}
	}
	self.table_sql_expiry_lock.Unlock()

	if expiredCount > 0 {
		log.Info("Cleaned up %d expired cache entries", expiredCount)
	}
}

// CleanupExpiredCacheAsync 异步清理过期缓存，在后台goroutine中运行
func (self *TCacher) CleanupExpiredCacheAsync(interval time.Duration) {
	if interval < time.Minute {
		interval = time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			self.CleanupExpiredCache()
		}
	}()

	log.Info("Started async cache cleanup with interval: %v", interval)
}

/*
func (self *TCacher) __SqlCacher(table string) (res_cacher cache.ICache) {
	if allowed := self.status[table]; allowed && self.active {
		var ok bool
		self.sql_caches_lock.RLock()
		res_cacher, ok = self.sql_caches[table]
		self.sql_caches_lock.RUnlock()

		if !ok {
			res_cacher = cache.NewCache("memory", fmt.Sprintf(`{"interval":%v,"expired":%v}`))
			self.sql_caches_lock.Lock()
			self.sql_caches[table] = res_cacher
			self.sql_caches_lock.Unlock()
		}
	}

	return
}

// Get all bean's ids according to sql and parameter from cache
func (self *TCacher) __RecCacher(table string) (res_cacher cache.ICache) {
	if allowed := self.status[table]; allowed && self.active {
		var ok bool
		self.id_caches_lock.RLock()
		res_cacher, ok = self.id_caches[table]
		self.id_caches_lock.RUnlock()

		if !ok {
			res_cacher = cache.NewCache("memory", `{"interval":5,"expired":4320}`)
			self.id_caches_lock.Lock()
			self.id_caches[table] = res_cacher
			self.id_caches_lock.Unlock()
		}

	}

	return
}
*/
