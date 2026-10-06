package orm

import (
	"fmt"
	"sync/atomic"

	"github.com/bwmarrin/snowflake"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
	//"github.com/google/uuid"
	//"github.com/rs/xid"
)

type (
	// Uuid field
	TIdField struct {
		TField
	}
)

// idNode 是本进程的雪花号生成器。
//
// id = 毫秒时间戳 << 22 | 节点号 << 12 | 毫秒内序号，序号每毫秒从 0 起。**节点号必须按进程唯一**：
// 两个节点号相同的进程在同一毫秒各建第一条记录，拿到的是同一个 id——同一张表就是一次莫名的
// 主键冲突，按租户分库之后则是搬租户时才炸的重号。默认的 1 只给单进程（测试、工具）用；
// 服务进程启动时由 vectors 从注册中心领一个唯一的节点号，经 SetIdNode 换上。
var idNode atomic.Pointer[snowflake.Node]

func init() {
	RegisterField("id", newIdField)
	if err := SetIdNode(1); err != nil {
		log.Err(err)
	}
}

// SetIdNode 把本进程的雪花号节点换成 n（0–1023）。之后生成的 id 都带这个节点号；可以在运行中
// 调用（节点租约丢了、换领一个新号时）。
func SetIdNode(n int64) error {
	node, err := snowflake.NewNode(n)
	if err != nil {
		return fmt.Errorf("orm: 雪花号节点 %d 不可用: %w", n, err)
	}
	idNode.Store(node)
	return nil
}

// NewId 生成一个新的记录 id。框架之外要自己建行的代码（定时任务表等）用它，别自己起一个
// 节点号写死的生成器——那会与本进程、别的进程撞号。
func NewId() int64 { return idNode.Load().Generate().Int64() }

func newIdField() IField {
	return new(TIdField)
}

func (self *TIdField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	model := ctx.Model
	/*field_name := fld.Name()

	// set type for field

		// set unique
		var index *TIndex
		var ok bool
		indexName := generate_index_name(UniqueType, model.Table(), []string{field_name})

		if index, ok = model.Obj().indexes[indexName]; ok {
			index.AddColumn(field_name)
		} else {
			index = newIndex(indexName, UniqueType)
			index.AddColumn(field_name)
			model.Obj().AddIndex(index)
		}
	*/
	field.isPrimaryKey = true
	field.isUnique = true
	field.required = true
	field.store = true

	//if field.SqlType.Name == "" {
	field.SqlType = SQLType{BigInt, 0, 0}
	field.typeName = "bigint"
	//}

	// set the id field for model
	model.IdField(field.Name())
}

func (self *TIdField) OnCreate(ctx *TFieldContext) any {
	return NewId()
}

// 转换值到字段输出数据类型
func (self *TIdField) onConvertToRead(session *TSession, cols []string, record []any, colIndex int) any {
	value := *record[colIndex].(*any)
	l := len(cols)
	if value == nil && l > 1 {
		node := domain.NewDomainNode()
		for I, name := range cols {
			// 过滤某些大内容的字段
			if name != self.Name() {
				fieldValue := *record[I].(*any)
				if !utils.IsBlank(fieldValue) {
					// only use those not null value as condition for where clause
					// because of null/0/nil maybe the same when read from database
					cond := domain.NewDomainNode(name, "=", fieldValue)
					node.AND(cond)
				}
			}
		}

		id := NewId()
		session.New().Set(session.Statement.IdKey, id).Domain(node).Limit(1).Write(nil) // 无需额外数据写入
		return id
	} else {
		return value2FieldTypeValue(self, value)
	}
}
