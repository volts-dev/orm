package core

import (
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
)

// Rows represents rows of table
type Rows struct {
	*sql.Rows
	db *DB
}

// ToMapString returns all records
func (rs *Rows) ToMapString() ([]map[string]string, error) {
	cols, err := rs.Columns()
	if err != nil {
		return nil, err
	}

	results := make([]map[string]string, 0, 10)
	for rs.Next() {
		record := make(map[string]string, len(cols))
		err = rs.ScanMap(&record)
		if err != nil {
			return nil, err
		}
		results = append(results, record)
	}

	if err = rs.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// ScanStructByIndex scan data to a struct's pointer according field index
func (rs *Rows) ScanStructByIndex(dest ...any) error {
	if len(dest) == 0 {
		return errors.New("at least one struct")
	}

	vvvs := make([]reflect.Value, len(dest))
	for i, s := range dest {
		vv := reflect.ValueOf(s)
		if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Struct {
			return errors.New("dest should be a struct's pointer")
		}

		vvvs[i] = vv.Elem()
	}

	cols, err := rs.Columns()
	if err != nil {
		return err
	}
	newDest := make([]any, len(cols))

	var i = 0
	for _, vvv := range vvvs {
		for j := 0; j < vvv.NumField(); j++ {
			newDest[i] = vvv.Field(j).Addr().Interface()
			i++
		}
	}

	return rs.Rows.Scan(newDest...)
}

var (
	fieldCache     = &sync.Map{} // 使用sync.Map避免竞态条件
	fieldCacheSize atomic.Int64  // 近似条目计数，用于防御性容量控制
)

// maxFieldCacheTypes 是 fieldCache 的防御性容量上限。key 是程序中实际的 struct 类型，
// 数量有限；此上限仅防止极端场景下无限增长。
const maxFieldCacheTypes = 4096

func fieldByName(v reflect.Value, name string) reflect.Value {
	t := v.Type()
	// 尝试从缓存中获取字段映射
	val, ok := fieldCache.Load(t)
	var cache map[string]int
	if ok {
		cache = val.(map[string]int)
	} else {
		// 缓存不存在，构建字段映射
		cache = make(map[string]int)
		for i := 0; i < v.NumField(); i++ {
			cache[t.Field(i).Name] = i
		}
		// 超过上限时清空，防止无限增长
		if fieldCacheSize.Load() >= maxFieldCacheTypes {
			fieldCache.Range(func(k, _ any) bool {
				fieldCache.Delete(k)
				return true
			})
			fieldCacheSize.Store(0)
		}
		// 使用LoadOrStore避免重复初始化，原子地存储
		actual, loaded := fieldCache.LoadOrStore(t, cache)
		if !loaded {
			fieldCacheSize.Add(1)
		}
		cache = actual.(map[string]int)
	}

	if i, ok := cache[name]; ok {
		return v.Field(i)
	}

	// 未命中返回无效 Value（而非 reflect.Zero(t)）：reflect.Zero(struct) 的 IsValid()
	// 为 true 但不可寻址，会让 ScanStructByName 误走 Addr() 分支 panic；返回无效 Value
	// 使其走 EmptyScanner 兜底分支，容忍结果集中存在 struct 没有的列。
	return reflect.Value{}
}

// ScanStructByName scan data to a struct's pointer according field name
func (rs *Rows) ScanStructByName(dest any) error {
	vv := reflect.ValueOf(dest)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Struct {
		return errors.New("dest should be a struct's pointer")
	}

	cols, err := rs.Columns()
	if err != nil {
		return err
	}

	newDest := make([]any, len(cols))
	var v EmptyScanner
	for j, name := range cols {
		f := fieldByName(vv.Elem(), rs.db.Mapper.Table2Obj(name))
		if f.IsValid() {
			newDest[j] = f.Addr().Interface()
		} else {
			newDest[j] = &v
		}
	}

	return rs.Rows.Scan(newDest...)
}

// ScanSlice scan data to a slice's pointer, slice's length should equal to columns' number
func (rs *Rows) ScanSlice(dest any) error {
	vv := reflect.ValueOf(dest)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Slice {
		return errors.New("dest should be a slice's pointer")
	}

	vvv := vv.Elem()
	cols, err := rs.Columns()
	if err != nil {
		return err
	}

	newDest := make([]any, len(cols))

	for j := 0; j < len(cols); j++ {
		if j >= vvv.Len() {
			newDest[j] = reflect.New(vvv.Type().Elem()).Interface()
		} else {
			newDest[j] = vvv.Index(j).Addr().Interface()
		}
	}

	err = rs.Rows.Scan(newDest...)
	if err != nil {
		return err
	}

	srcLen := vvv.Len()
	for i := srcLen; i < len(cols); i++ {
		vvv = reflect.Append(vvv, reflect.ValueOf(newDest[i]).Elem())
	}
	return nil
}

// ScanMap scan data to a map's pointer
func (rs *Rows) ScanMap(dest any) error {
	vv := reflect.ValueOf(dest)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Map {
		return errors.New("dest should be a map's pointer")
	}

	cols, err := rs.Columns()
	if err != nil {
		return err
	}

	newDest := make([]any, len(cols))
	vvv := vv.Elem()

	for i := range cols {
		// 每列分配独立内存：旧实现用挂在共享 *DB 上的环形缓冲（reflectNew），
		// 并发查询会对同一底层数组元素无同步并发写（data race）。
		newDest[i] = reflect.New(vvv.Type().Elem()).Interface()
	}

	err = rs.Rows.Scan(newDest...)
	if err != nil {
		return err
	}

	for i, name := range cols {
		vname := reflect.ValueOf(name)
		vvv.SetMapIndex(vname, reflect.ValueOf(newDest[i]).Elem())
	}

	return nil
}

// Row reprents a row of  a tab
type Row struct {
	rows *Rows
	// One of these two will be non-nil:
	err error // deferred error for easy chaining
}

// ErrorRow return an error row
func ErrorRow(err error) *Row {
	return &Row{
		err: err,
	}
}

// NewRow from rows
func NewRow(rows *Rows, err error) *Row {
	return &Row{rows, err}
}

// Columns returns all columns of the row
func (row *Row) Columns() ([]string, error) {
	if row.err != nil {
		return nil, row.err
	}
	return row.rows.Columns()
}

// Scan retrieves all row column values
func (row *Row) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	defer row.rows.Close()

	for _, dp := range dest {
		if _, ok := dp.(*sql.RawBytes); ok {
			return errors.New("sql: RawBytes isn't allowed on Row.Scan")
		}
	}

	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	err := row.rows.Scan(dest...)
	if err != nil {
		return err
	}
	// Make sure the query can be processed to completion with no errors.
	return row.rows.Close()
}

// ScanStructByName retrieves all row column values into a struct
func (row *Row) ScanStructByName(dest any) error {
	if row.err != nil {
		return row.err
	}
	defer row.rows.Close()

	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	err := row.rows.ScanStructByName(dest)
	if err != nil {
		return err
	}
	// Make sure the query can be processed to completion with no errors.
	return row.rows.Close()
}

// ScanStructByIndex retrieves all row column values into a struct
func (row *Row) ScanStructByIndex(dest any) error {
	if row.err != nil {
		return row.err
	}
	defer row.rows.Close()

	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	err := row.rows.ScanStructByIndex(dest)
	if err != nil {
		return err
	}
	// Make sure the query can be processed to completion with no errors.
	return row.rows.Close()
}

// ScanSlice scan data to a slice's pointer, slice's length should equal to columns' number
func (row *Row) ScanSlice(dest any) error {
	if row.err != nil {
		return row.err
	}
	defer row.rows.Close()

	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	err := row.rows.ScanSlice(dest)
	if err != nil {
		return err
	}

	// Make sure the query can be processed to completion with no errors.
	return row.rows.Close()
}

// ScanMap scan data to a map's pointer
func (row *Row) ScanMap(dest any) error {
	if row.err != nil {
		return row.err
	}
	defer row.rows.Close()

	if !row.rows.Next() {
		if err := row.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	err := row.rows.ScanMap(dest)
	if err != nil {
		return err
	}

	// Make sure the query can be processed to completion with no errors.
	return row.rows.Close()
}

// ToMapString returns all clumns of this record
func (row *Row) ToMapString() (map[string]string, error) {
	cols, err := row.Columns()
	if err != nil {
		return nil, err
	}

	var record = make(map[string]string, len(cols))
	err = row.ScanMap(&record)
	if err != nil {
		return nil, err
	}

	return record, nil
}
