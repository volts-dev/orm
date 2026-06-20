package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"regexp"
)

var (
	// DefaultCacheSize sets the default cache size
	DefaultCacheSize = 200
)

// MapToSlice map query and struct as sql and args
func MapToSlice(query string, mp any) (string, []any, error) {
	if query == "" {
		return "", []any{}, nil
	}

	vv := reflect.ValueOf(mp)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Map {
		return "", []any{}, ErrNoMapPointer
	}

	mapElem := vv.Elem()
	mapKeys := mapElem.MapKeys()
	args := make([]any, 0, len(mapKeys))
	var err error

	query = re.ReplaceAllStringFunc(query, func(src string) string {
		if err != nil {
			return ""
		}

		key := src[1:]
		v := mapElem.MapIndex(reflect.ValueOf(key))
		if !v.IsValid() {
			err = fmt.Errorf("map key %s is missing", key)
			return ""
		}

		args = append(args, v.Interface())
		return "?"
	})

	if err != nil {
		return "", []any{}, err
	}

	return query, args, nil
}

// StructToSlice converts a query and struct as sql and args
func StructToSlice(query string, st any) (string, []any, error) {
	vv := reflect.ValueOf(st)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Struct {
		return "", []any{}, ErrNoStructPointer
	}

	args := make([]any, 0)
	var err error
	query = re.ReplaceAllStringFunc(query, func(src string) string {
		fvv := vv.Elem().FieldByName(src[1:])
		if !fvv.IsValid() { // 命名参数无对应结构体字段：返回错误而非 panic（与 MapToSlice 行为一致）
			err = fmt.Errorf("struct field %s is missing", src[1:])
			return "?"
		}
		fv := fvv.Interface()
		if v, ok := fv.(driver.Valuer); ok {
			var value driver.Value
			value, err = v.Value()
			if err != nil {
				return "?"
			}
			args = append(args, value)
		} else {
			args = append(args, fv)
		}
		return "?"
	})
	if err != nil {
		return "", []any{}, err
	}

	return query, args, nil
}

var (
	_ QueryExecuter = &DB{}
)

// DB is a wrap of sql.DB with extra contents
type DB struct {
	*sql.DB
	Mapper IMapper
	hooks  Hooks
}

// Open opens a database
func Open(driverName, dataSourceName string) (*DB, error) {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	return &DB{
		DB:     db,
		Mapper: NewCacheMapper(&SnakeMapper{}),
	}, nil
}

// FromDB creates a DB from a sql.DB
func FromDB(db *sql.DB) *DB {
	return &DB{
		DB:     db,
		Mapper: NewCacheMapper(&SnakeMapper{}),
	}
}

// NeedLogSQL returns true if need to log SQL
func (db *DB) NeedLogSQL(ctx context.Context) bool {
	if log == nil {
		return false
	}
	/*
		v := ctx.Value(log.SessionShowSQLKey)
		if showSQL, ok := v.(bool); ok {
			return showSQL
		}
		return db.Logger.IsShowSQL()*/
	return false
}


// QueryContext overwrites sql.DB.QueryContext
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*Rows, error) {
	hookCtx := NewContextHook(ctx, query, args)
	ctx, err := db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}

	rows, err := db.DB.QueryContext(ctx, query, args...)
	hookCtx.End(ctx, nil, err)
	if err := db.afterProcess(hookCtx); err != nil {
		if rows != nil {
			rows.Close()
		}
		return nil, err
	}

	return &Rows{rows, db}, nil
}

// Query overwrites sql.DB.Query
func (db *DB) Query(query string, args ...any) (*Rows, error) {
	return db.QueryContext(context.Background(), query, args...)
}

// QueryMapContext executes query with parameters via map and context
func (db *DB) QueryMapContext(ctx context.Context, query string, mp any) (*Rows, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}

	return db.QueryContext(ctx, query, args...)
}

// QueryMap executes query with parameters via map
func (db *DB) QueryMap(query string, mp any) (*Rows, error) {
	return db.QueryMapContext(context.Background(), query, mp)
}

// QueryStructContext query rows with struct
func (db *DB) QueryStructContext(ctx context.Context, query string, st any) (*Rows, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}

	return db.QueryContext(ctx, query, args...)
}

// QueryStruct query rows with struct
func (db *DB) QueryStruct(query string, st any) (*Rows, error) {
	return db.QueryStructContext(context.Background(), query, st)
}

// QueryRowContext query row with args
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *Row {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return &Row{nil, err}
	}

	return &Row{rows, nil}
}

// QueryRow query row with args
func (db *DB) QueryRow(query string, args ...any) *Row {
	return db.QueryRowContext(context.Background(), query, args...)
}

// QueryRowMapContext query row with map
func (db *DB) QueryRowMapContext(ctx context.Context, query string, mp any) *Row {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return &Row{nil, err}
	}

	return db.QueryRowContext(ctx, query, args...)
}

// QueryRowMap query row with map
func (db *DB) QueryRowMap(query string, mp any) *Row {
	return db.QueryRowMapContext(context.Background(), query, mp)
}

// QueryRowStructContext query row with struct
func (db *DB) QueryRowStructContext(ctx context.Context, query string, st any) *Row {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return &Row{nil, err}
	}

	return db.QueryRowContext(ctx, query, args...)
}

// QueryRowStruct query row with struct
func (db *DB) QueryRowStruct(query string, st any) *Row {
	return db.QueryRowStructContext(context.Background(), query, st)
}

var (
	re = regexp.MustCompile(`[?](\w+)`)
)

// ExecMapContext exec map with context.ContextHook
// insert into (name) values (?)
// insert into (name) values (?name)
func (db *DB) ExecMapContext(ctx context.Context, query string, mp any) (sql.Result, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}

	return db.ExecContext(ctx, query, args...)
}

// ExecMap exec query with map
func (db *DB) ExecMap(query string, mp any) (sql.Result, error) {
	return db.ExecMapContext(context.Background(), query, mp)
}

// ExecStructContext exec query with map
func (db *DB) ExecStructContext(ctx context.Context, query string, st any) (sql.Result, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}

	return db.ExecContext(ctx, query, args...)
}

// ExecContext exec query with args
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	hookCtx := NewContextHook(ctx, query, args)
	ctx, err := db.beforeProcess(hookCtx)
	if err != nil {
		return nil, err
	}

	res, err := db.DB.ExecContext(ctx, query, args...)
	hookCtx.End(ctx, res, err)
	if err := db.afterProcess(hookCtx); err != nil {
		return nil, err
	}

	return res, nil
}

// ExecStruct exec query with struct
func (db *DB) ExecStruct(query string, st any) (sql.Result, error) {
	return db.ExecStructContext(context.Background(), query, st)
}

func (db *DB) beforeProcess(c *ContextHook) (context.Context, error) {
	// TODO: db.Logger.BeforeSQL(log.LogContext(*c)) when NeedLogSQL
	ctx, err := db.hooks.BeforeProcess(c)
	if err != nil {
		return nil, err
	}

	return ctx, nil
}

func (db *DB) afterProcess(c *ContextHook) error {
	err := db.hooks.AfterProcess(c)
	// TODO: db.Logger.AfterSQL(log.LogContext(*c)) when NeedLogSQL
	return err
}

// AddHook adds hook
func (db *DB) AddHook(h ...Hook) {
	db.hooks.AddHook(h...)
}
