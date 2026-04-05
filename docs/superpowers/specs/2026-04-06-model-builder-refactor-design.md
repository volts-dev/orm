# ModelBuilder Refactor Design

**Date**: 2026-04-06  
**File**: `model_builder.go`  
**Goal**: Extend `ModelBuilder` and `fieldStatment` to cover every ORM struct tag, allowing complete table definition via `OnBuildFields` with only a minimal struct (`type UserModel struct { orm.TModel }`), no struct field tags required.

---

## Background

The ORM currently supports two definition styles:

1. **Struct tags** — fields annotated with `field:"pk autoincr title('ID') index"` etc.
2. **Builder** (`OnBuildFields`) — fluent API via `ModelBuilder` and `fieldStatment`

Style 2 is incomplete: several field types and field attributes available as tags have no Builder equivalent. This forces models that want to use Builder-only mode to fall back on tags for certain features.

---

## Goal

After this refactor, a complete table definition is possible with zero struct field tags:

```go
type UserModel struct { orm.TModel }

func (m *UserModel) OnBuildFields(mb *orm.ModelBuilder) {
    mb.TableName("user_model").
        TableDescription("System users").
        TableOrder("name")

    mb.IdField().AutoIncrement()
    mb.VarcharField("name").Required().Size(128).Title("Name").IsUnique()
    mb.FloatField("score").Default(0.0)
    mb.BinaryField("avatar").Attachment()
    mb.JsonField("meta").Store(true)
    mb.DateField("birthday")
    mb.BoolField("active").Default(true).Named()
    mb.SelectionField("role", func() [][]string {
        return [][]string{{"admin", "Admin"}, {"user", "User"}}
    })
}
```

Struct tags continue to work unchanged (full backward compatibility).

---

## Approach

**Extend only `model_builder.go`.** No other files change.

- Add missing field type shortcut methods on `ModelBuilder`
- Add missing field attribute methods on `fieldStatment`
- Add table-level configuration methods on `ModelBuilder`
- All new methods delegate to the existing `tag_*` functions to avoid duplicating logic

---

## Section 1 — New Field Type Methods (on `ModelBuilder`)

| Method | Field Type String | Notes |
|---|---|---|
| `FloatField(name string)` | `"float"` | |
| `DateField(name string)` | `"date"` | |
| `BinaryField(name string)` | `"binary"` | |
| `JsonField(name string)` | `"json"` | |

Implementation pattern (same as existing `IntField`, `TextField`, etc.):

```go
func (self *ModelBuilder) FloatField(name string) *fieldStatment {
    return self.Field(name, "float")
}
```

---

## Section 2 — New Field Attribute Methods (on `fieldStatment`)

| Method | Tag Equivalent | Implementation |
|---|---|---|
| `AutoIncrement()` | `autoincr` | `field.isAutoIncrement = true` |
| `WriteOnly()` | `->` | calls `tag_write_only(ctx)` |
| `Translate(v ...bool)` | `translate` | calls `tag_translate(ctx)` |
| `Attachment()` | `attachment` | calls `tag_attachment(ctx)` |
| `Version()` | `version` | calls `tag_ver(ctx)` |
| `Named()` | `named` | calls `tag_named(ctx)` |
| `OldName(name string)` | `oldname` | sets `field._attr_name = name` (rename hint) |
| `Deleted()` | `deleted` | calls `tag_deleted(ctx)` |
| `As(alias string)` | `as` | calls `tag_as(ctx)` |

Methods that call existing `tag_*` functions construct a `TTagContext` using the builder's `Orm`, `Model`, `modelValue`, and the field — same pattern used by existing `IsUnique()`, `IsIndex()`, `IsCreated()`, `IsUpdated()`.

`AutoIncrement()` sets the flag directly (same as `tag_auto`) and also updates `model.Obj().AutoIncrementField` to match what the tag handler does.

---

## Section 3 — New Table-Level Methods (on `ModelBuilder`)

Table-level methods operate on the model rather than a field. They construct a `TTagContext` with `Field: nil` (table tags do not use the Field parameter).

| Method | Tag Equivalent | Implementation |
|---|---|---|
| `TableName(name string)` | `table_name` | calls `tag_table_name(ctx)` with `Params: []string{name}` |
| `TableDescription(desc string)` | `table_description` | calls `tag_table_description(ctx)` with `Params: []string{desc}` |
| `TableOrder(field string)` | `table_order` | calls `tag_table_order(ctx)` with `Params: []string{field}` |
| `TableExtends(relateField string)` | `table_extends` | calls `tag_table_extends(ctx)` |
| `TableRelate(modelName, relateField string)` | `table_relate` | calls `tag_table_relate(ctx)` with `Params: []string{modelName, relateField}` |

All table-level methods return `*ModelBuilder` (not `*fieldStatment`) to allow chaining.

`TableExtends` is a special case: `tag_table_extends` uses `ctx.FieldTypeValue` (the reflect.Value of an embedded struct). The Builder version accepts a reflect.Value directly since there is no struct field to derive it from.

---

## Backward Compatibility

- All existing struct tag processing is unchanged
- All existing `ModelBuilder` and `fieldStatment` methods are unchanged (no signature changes)
- Mixing tags and Builder on the same model continues to work

---

## Out of Scope

- Changing how `OnBuildFields` is called (no signature change)
- `table_extends` full implementation (existing tag handler is TODO — Builder wrapper exposes same incomplete behavior)
- Many2many intermediate table auto-creation (tracked separately)
- Any changes outside `model_builder.go`
