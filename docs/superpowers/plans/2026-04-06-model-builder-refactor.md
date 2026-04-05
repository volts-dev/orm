# ModelBuilder Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `ModelBuilder` and `fieldStatment` in `model_builder.go` to cover every ORM struct tag, enabling complete table definition via `OnBuildFields` with no struct field tags.

**Architecture:** Single-file change — add methods to `ModelBuilder` (field types + table-level) and `fieldStatment` (field attributes). All new methods delegate to existing `tag_*` functions to avoid duplicating logic.

**Tech Stack:** Go, `github.com/volts-dev/orm` internal types (`TTagContext`, `IField`, `TModel`, `TOrm`)

---

## File Map

- Modify: `model_builder.go` — all changes in this file only
- Reference (read-only): `tag.go` — existing `tag_*` functions to delegate to
- Reference (read-only): `field.go` — `TField` struct fields (`isAutoIncrement`, `_attr_name`, etc.)

---

### Task 1: Add Missing Field Type Methods on `ModelBuilder`

Add four field type shortcut methods following the exact same pattern as existing `IntField`, `TextField`, etc.

**Files:**
- Modify: `model_builder.go`

- [ ] **Step 1: Read current state of `model_builder.go`**

Confirm the last method in the file (currently `Ondelete` at line 374) and the import block.

- [ ] **Step 2: Add the four field type methods after `ManyToManyField`**

Add at line ~195 (before the `fieldStatment` methods), after `ManyToManyField`:

```go
func (self *ModelBuilder) FloatField(name string) *fieldStatment {
	return self.Field(name, "float")
}

func (self *ModelBuilder) DateField(name string) *fieldStatment {
	return self.Field(name, "date")
}

func (self *ModelBuilder) BinaryField(name string) *fieldStatment {
	return self.Field(name, "binary")
}

func (self *ModelBuilder) JsonField(name string) *fieldStatment {
	return self.Field(name, "json")
}
```

- [ ] **Step 3: Verify the file compiles**

```bash
cd /Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add model_builder.go
git commit -m "feat(builder): add FloatField, DateField, BinaryField, JsonField type methods"
```

---

### Task 2: Add Missing Field Attribute Methods on `fieldStatment`

Add nine attribute methods following the pattern of existing methods like `IsUnique()`, `IsIndex()`, `IsCreated()`.

The pattern for methods that delegate to `tag_*`:

```go
func (self *fieldStatment) SomeMethod() *fieldStatment {
    field := self.field
    builder := self.builder
    if err := tag_some_func(&TTagContext{
        Orm:        builder.Orm,
        Model:      builder.model,
        Field:      field,
        ModelValue: builder.model.modelValue,
    }); err != nil {
        log.Warn(err.Error())
    }
    return self
}
```

**Files:**
- Modify: `model_builder.go`

- [ ] **Step 1: Add `AutoIncrement()` after `Ondelete` (end of file)**

`tag_auto` sets `field.isAutoIncrement = true`. The Builder version also updates `model.Obj().AutoIncrementField` so the model object knows which field auto-increments (same as `tag_table_extends` does when copying fields).

```go
func (self *fieldStatment) AutoIncrement() *fieldStatment {
	field := self.field.Base()
	field.isAutoIncrement = true
	self.builder.model.Obj().AutoIncrementField = self.field.Name()
	return self
}
```

- [ ] **Step 2: Add `WriteOnly()`**

```go
func (self *fieldStatment) WriteOnly() *fieldStatment {
	field := self.field
	builder := self.builder
	if err := tag_write_only(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 3: Add `Translate(v ...bool)`**

```go
func (self *fieldStatment) Translate(v ...bool) *fieldStatment {
	field := self.field
	builder := self.builder
	params := []string{}
	if len(v) > 0 {
		if v[0] {
			params = []string{"true"}
		} else {
			params = []string{"false"}
		}
	}
	if err := tag_translate(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
		Params:     params,
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 4: Add `Attachment()`**

```go
func (self *fieldStatment) Attachment() *fieldStatment {
	field := self.field
	builder := self.builder
	if err := tag_attachment(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 5: Add `Version()`**

```go
func (self *fieldStatment) Version() *fieldStatment {
	field := self.field
	builder := self.builder
	if err := tag_ver(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 6: Add `Named()`**

```go
func (self *fieldStatment) Named() *fieldStatment {
	field := self.field
	builder := self.builder
	if err := tag_named(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 7: Add `OldName(name string)`**

`tag_old_name` in `tag.go` is currently a no-op (`return nil`). The rename hint is stored in `field._attr_name`. Set it directly:

```go
func (self *fieldStatment) OldName(name string) *fieldStatment {
	self.field.Base()._attr_name = name
	return self
}
```

- [ ] **Step 8: Add `Deleted()`**

```go
func (self *fieldStatment) Deleted() *fieldStatment {
	field := self.field
	builder := self.builder
	if err := tag_deleted(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 9: Add `As(alias string)`**

```go
func (self *fieldStatment) As(alias string) *fieldStatment {
	field := self.field
	builder := self.builder
	if err := tag_as(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
		Params:     []string{alias},
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 10: Verify the file compiles**

```bash
cd /Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm && go build ./...
```

Expected: no errors.

- [ ] **Step 11: Commit**

```bash
git add model_builder.go
git commit -m "feat(builder): add AutoIncrement, WriteOnly, Translate, Attachment, Version, Named, OldName, Deleted, As attribute methods"
```

---

### Task 3: Add Table-Level Methods on `ModelBuilder`

Add five table-level configuration methods. These operate on the model (not a field), return `*ModelBuilder` for chaining, and construct `TTagContext` with a nil-safe field.

Note: `tag_table_name`, `tag_table_description`, `tag_table_order`, `tag_table_relate` do not use `ctx.Field` — safe to pass `nil`. `tag_table_extends` uses `ctx.FieldTypeValue` — the Builder version accepts a `reflect.Value` directly.

**Files:**
- Modify: `model_builder.go`

- [ ] **Step 1: Add `TableName(name string)` after `SetIndex`**

Add after `SetIndex` method (around line 44):

```go
func (self *ModelBuilder) TableName(name string) *ModelBuilder {
	if err := tag_table_name(&TTagContext{
		Orm:    self.Orm,
		Model:  self.model,
		Params: []string{name},
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 2: Add `TableDescription(desc string)`**

```go
func (self *ModelBuilder) TableDescription(desc string) *ModelBuilder {
	if err := tag_table_description(&TTagContext{
		Orm:    self.Orm,
		Model:  self.model,
		Params: []string{desc},
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 3: Add `TableOrder(field string)`**

```go
func (self *ModelBuilder) TableOrder(field string) *ModelBuilder {
	if err := tag_table_order(&TTagContext{
		Orm:    self.Orm,
		Model:  self.model,
		Params: []string{field},
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 4: Add `TableRelate(modelName, relateField string)`**

```go
func (self *ModelBuilder) TableRelate(modelName, relateField string) *ModelBuilder {
	if err := tag_table_relate(&TTagContext{
		Orm:    self.Orm,
		Model:  self.model,
		Params: []string{modelName, relateField},
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

- [ ] **Step 5: Add `TableExtends(fieldTypeValue reflect.Value, relateField string)`**

`tag_table_extends` reads `ctx.FieldTypeValue` and `ctx.Params[0]`. The Builder exposes this directly. Add `"reflect"` to imports.

```go
func (self *ModelBuilder) TableExtends(fieldTypeValue reflect.Value, relateField string) *ModelBuilder {
	if err := tag_table_extends(&TTagContext{
		Orm:            self.Orm,
		Model:          self.model,
		FieldTypeValue: fieldTypeValue,
		Params:         []string{relateField},
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}
```

Check whether `reflect` is already imported in `model_builder.go`. If not, add it to the import block.

- [ ] **Step 6: Verify the file compiles**

```bash
cd /Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm && go build ./...
```

Expected: no errors.

- [ ] **Step 7: Run existing tests to confirm no regressions**

```bash
cd /Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm && go test ./test/ -run TestORMInterfaces -v -count=1 2>&1 | tail -30
```

Expected: all subtests pass (same result as before this change).

- [ ] **Step 8: Commit**

```bash
git add model_builder.go
git commit -m "feat(builder): add TableName, TableDescription, TableOrder, TableRelate, TableExtends table-level methods"
```

---

## Self-Review

**Spec coverage:**
- ✅ 4 new field types (FloatField, DateField, BinaryField, JsonField) — Task 1
- ✅ 9 new field attributes (AutoIncrement, WriteOnly, Translate, Attachment, Version, Named, OldName, Deleted, As) — Task 2
- ✅ 5 table-level methods (TableName, TableDescription, TableOrder, TableRelate, TableExtends) — Task 3
- ✅ Backward compat — no existing signatures changed
- ✅ Only `model_builder.go` modified

**Placeholder scan:** No TBD or TODO in tasks. All code blocks are complete.

**Type consistency:** `TTagContext`, `IField`, `*ModelBuilder`, `*fieldStatment` used consistently across all tasks. `tag_*` function names match `tag.go` exactly: `tag_write_only`, `tag_translate`, `tag_attachment`, `tag_ver`, `tag_named`, `tag_deleted`, `tag_as`, `tag_table_name`, `tag_table_description`, `tag_table_order`, `tag_table_relate`, `tag_table_extends`.
