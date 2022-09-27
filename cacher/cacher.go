package cacher

import (
	"fmt"
	"sync"

	"github.com/volts-dev/cacher"
	"github.com/volts-dev/dataset"
)

// TODO cache name
type (
	TCacher struct {
		sync.RWMutex
		active   bool
		interval int
		expired  int
		status   map[string]bool
		//TODO 将表名和缓存Key 组合为table_key
		id_caches                cacher.ICacher // 缓存Id 对应记录 map[model]record
		sql_caches               cacher.ICacher // 缓存Sq
		table_id_key_index       map[string]map[string]bool
		table_sql_key_index      map[string]map[string]bool
		table_id_key_index_lock  sync.RWMutex
		table_sql_key_index_lock sync.RWMutex
	}
)

func NewCacher() *TCacher {
	chr := &TCacher{
		status:              make(map[string]bool),
		table_id_key_index:  make(map[string]map[string]bool),
		table_sql_key_index: make(map[string]map[string]bool),
	}
	var err error

	chr.id_caches, err = cacher.New("memory")
	if err != nil {
		fmt.Println(err)
	}

	chr.sql_caches, err = cacher.New("memory")
	if err != nil {
		fmt.Println(err)
	}

	return chr
}

//@removed 是否用于移除
func (self *TCacher) genIdKey(table string, key interface{}, removed bool) string {
	str := fmt.Sprintf("%v-%v", table, key)

	self.table_id_key_index_lock.Lock()
	defer self.table_id_key_index_lock.Unlock()

	// # 添加索引
	var (
		tb  map[string]bool
		has bool
	)
	if tb, has = self.table_id_key_index[table]; !has {
		tb = make(map[string]bool)
		self.table_id_key_index[table] = tb
	}

	// #移除索引
	if removed {
		delete(tb, str)
		return str
	} else {
		tb[str] = true
	}

	return str
}

func (self *TCacher) genSqlKey(table string, sql string, args interface{}, removed bool) string {
	str := fmt.Sprintf("%v-%v-%v", table, sql, args)
	// # 添加索引
	var (
		tb  map[string]bool
		has bool
	)

	//# lock
	self.table_sql_key_index_lock.Lock()
	defer self.table_sql_key_index_lock.Unlock()

	if tb, has = self.table_sql_key_index[table]; !has {
		tb = make(map[string]bool)
		self.table_sql_key_index[table] = tb
	}

	// #移除索引
	if removed {
		delete(tb, str)
		return str
	} else {
		tb[str] = true
	}

	return str
}

// turn on the cacher for query
func (self *TCacher) Active(sw bool) {
	self.active = sw
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

//#缓存Sql查询结果ID集
func (self *TCacher) PutBySql(table string, sql string, arg interface{}, data *dataset.TDataSet) {
	if open, has := self.status[table]; has && open {
		key := self.genSqlKey(table, sql, arg, false)
		self.sql_caches.Set(&cacher.CacheBlock{Key: key, Value: data})
	}
}

//#通过Sql获取查询结果ID集
// @Return:  nil or 空[]string
func (self *TCacher) GetBySql(table string, sql string, arg interface{}) *dataset.TDataSet {
	//逻辑可能有问题	if open, has := self.status[table]; !has || (has && open) {
	if open, has := self.status[table]; has && open {
		key := self.genSqlKey(table, sql, arg, false)

		var ids *dataset.TDataSet
		if err := self.sql_caches.Get(key, &ids); err != nil {
			return nil
		}
		return ids
	}

	return nil
}

// #缓存记录及ID
func (self *TCacher) PutById(table string, id interface{}, record *dataset.TRecordSet) {
	if open, has := self.status[table]; !has || (has && open) {
		//ck := self.RecCacher(table)
		key := self.genIdKey(table, id, false)
		self.id_caches.Set(&cacher.CacheBlock{Key: key, Value: record})
	}
}

//#通过ID获取记录
func (self *TCacher) GetByIds(table string, ids ...interface{}) (records []*dataset.TRecordSet, ids_less []interface{}) {
	if !self.active {
		return nil, ids
	}

	if open, has := self.status[table]; !has || (has && open) {
		for _, id := range ids {
			key := self.genIdKey(table, id, false)

			var rec *dataset.TRecordSet
			if err := self.sql_caches.Get(key, &ids); err != nil {
				ids_less = append(ids_less, id)
			}
			records = append(records, rec)
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
			key := self.genIdKey(table, id, true)

			self.id_caches.Delete(key)
		}
	}
}

func (self *TCacher) RemoveBySql(table string, sqls ...string) {
	self.table_id_key_index_lock.Lock()
	defer self.table_id_key_index_lock.Unlock()
	if _, has := self.table_sql_key_index[table]; has {
		for _, sql := range sqls {
			key := self.genIdKey(table, sql, true)

			self.sql_caches.Delete(key)
		}
	}
}

//
func (self *TCacher) ClearByTable(table string) {
	self.table_id_key_index_lock.Lock()
	defer self.table_id_key_index_lock.Unlock()
	if m, has := self.table_id_key_index[table]; has {
		for key := range m {
			self.sql_caches.Delete(key)
		}
	}
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
