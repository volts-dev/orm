package orm

import (
	"fmt"
	"sync"
	"vectors/cacher"
)

type (
	TCacher struct {
		active   bool
		interval int
		expired  int
		status   map[string]bool

		//TODO 将表名和缓存Key 组合为table_key
		// #cache
		//id_caches       map[string]cache.ICache // 缓存Id 对应记录 map[model]record
		//sql_caches      map[string]cache.ICache // 缓存Sql  map[model]ids
		id_caches                cache.ICacher // 缓存Id 对应记录 map[model]record
		sql_caches               cache.ICacher // 缓存Sq
		table_id_key_index       map[string]map[string]bool
		table_sql_key_index      map[string]map[string]bool
		table_id_key_index_lock  sync.RWMutex
		table_sql_key_index_lock sync.RWMutex
		//id_caches_lock  sync.RWMutex
		//sql_caches_lock sync.RWMutex
	}
)

func NewCacher() *TCacher {
	cacher := &TCacher{
		status:              make(map[string]bool),
		table_id_key_index:  make(map[string]map[string]bool),
		table_sql_key_index: make(map[string]map[string]bool),
	}
	var err error
	cacher.id_caches, err = cache.NewCacher("memory", fmt.Sprintf(`{"interval":%v,"expired":%v}`))
	if err != nil {
		fmt.Println(err)
	}

	cacher.sql_caches, err = cache.NewCacher("memory", fmt.Sprintf(`{"interval":%v,"expired":%v}`))
	if err != nil {
		fmt.Println(err)
	}

	return cacher
}

//@removed 是否用于移除
func (self *TCacher) genIdKey(table string, key string, removed bool) string {
	str := fmt.Sprintf("%v-%v", table, key)

	// # 添加索引
	var (
		tb  map[string]bool
		has bool
	)

	self.table_id_key_index_lock.Lock()
	defer self.table_id_key_index_lock.Unlock()
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
func (self *TCacher) PutBySql(table string, sql string, arg interface{}, record_ids ...string) {
	if open, has := self.status[table]; has && open {
		key := self.genSqlKey(table, sql, arg, false)
		self.sql_caches.Put(key, record_ids)
	}
}

//#通过Sql获取查询结果ID集
// result =nil or 空[]string
func (self *TCacher) GetBySql(table string, sql string, arg interface{}) (res_ids []string) {
	//逻辑可能有问题	if open, has := self.status[table]; !has || (has && open) {
	if open, has := self.status[table]; has && open {
		key := self.genSqlKey(table, sql, arg, false)
		if ids, ok := self.sql_caches.Get(key).([]string); ok {
			return ids
		} else {
			return nil
		}
	}

	return nil
}

// #缓存记录及ID
func (self *TCacher) PutById(table string, id string, record *TRecordSet) {
	if open, has := self.status[table]; !has || (has && open) {
		//ck := self.RecCacher(table)
		key := self.genIdKey(table, id, false)
		self.id_caches.Put(key, record)
	}
}

//#通过ID获取记录
func (self *TCacher) GetByIds(table string, ids ...string) (records []*TRecordSet, ids_less []string) {
	if !self.active {
		return nil, ids
	}

	if open, has := self.status[table]; !has || (has && open) {
		for _, id := range ids {
			key := self.genIdKey(table, id, false)
			if rec, ok := self.id_caches.Get(key).(*TRecordSet); ok {
				records = append(records, rec)
			} else {
				ids_less = append(ids_less, id)
			}
		}

		return records, ids_less
	} else {
		return nil, ids
	}

	return nil, nil
}

func (self *TCacher) RemoveById(table string, ids ...string) {
	if _, has := self.table_id_key_index[table]; has {
		for _, id := range ids {
			key := self.genIdKey(table, id, true)

			self.id_caches.Remove(key)
		}
	}
}

func (self *TCacher) RemoveBySql(table string, sqls ...string) {
	if _, has := self.table_sql_key_index[table]; has {
		for _, sql := range sqls {
			key := self.genIdKey(table, sql, true)

			self.sql_caches.Remove(key)
		}
	}
}

//
func (self *TCacher) ClearByTable(table string) {
	if m, has := self.table_id_key_index[table]; has {
		for key, _ := range m {
			self.sql_caches.Remove(key)
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
