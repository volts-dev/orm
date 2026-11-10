package orm

import (
	"container/list"
	"strings"
	"sync"
)

// Only request-independent built-in attributes participate. The complete shape is
// the key, not a model/version label: runtime scalar defaults and field mutations
// must be visible on the next call without a TTL or a lifecycle notification.
type fieldAttributeShape struct {
	name, kind, help, label, typeName, related, groups, domain, relationModel, relationKey string
	digitsUsage, minDigitsUsage, groupOperator                                             string
	store, manual, readonly, required, sortable, searchable, indexed, translate, inherited bool
	size                                                                                   int
	digits                                                                                 [2]int
	hasDigits, hasMinDigits                                                                bool
	minDigits                                                                              int
	defaultValue                                                                           any
	relational                                                                             bool
}
type fieldAttributeEntry struct {
	key   fieldAttributeShape
	attrs map[string]any
	bytes int64
}
type fieldAttributeStore struct {
	sync.RWMutex
	entries    map[fieldAttributeShape]*list.Element
	order      *list.List
	bytes      int64
	generation uint64
}

var fieldAttributes = &fieldAttributeStore{entries: make(map[fieldAttributeShape]*list.Element), order: list.New()}

const fieldAttributeMaxEntries = 8192
const fieldAttributeMaxBytes = 16 << 20
const fieldAttributeMaxEntryBytes = 64 << 10

// The host may attach these recomputable entries to its existing memory controller.
// Keep this ORM compatible with its pinned framework version (no new dependency).
func (*TField) FieldAttributeCacheUsage() (int, int64) {
	c := fieldAttributes
	c.RLock()
	defer c.RUnlock()
	return len(c.entries), c.bytes
}
func (*TField) ReclaimFieldAttributeCache(all bool) (int, int64) { return fieldAttributes.reclaim(all) }

func (c *fieldAttributeStore) removeFirst() (int, int64) {
	front := c.order.Front()
	if front == nil {
		return 0, 0
	}
	entry := front.Value.(*fieldAttributeEntry)
	delete(c.entries, entry.key)
	c.order.Remove(front)
	c.bytes -= entry.bytes
	return 1, entry.bytes
}
func (c *fieldAttributeStore) reclaim(all bool) (int, int64) {
	c.Lock()
	defer c.Unlock()
	c.generation++
	count := len(c.entries)
	if !all {
		count = (count + 3) / 4
	}
	entries := 0
	bytes := int64(0)
	for i := 0; i < count; i++ {
		n, size := c.removeFirst()
		entries += n
		bytes += size
	}
	return entries, bytes
}

func cachedAttributeKind(field IField) (string, bool) {
	switch field.(type) {
	case *TField, *TCharField, *TVarcharField, *TTextField, *TNameField, *TIdField, *TIntField, *TBigIntField, *TFloatField, *TDoubleField, *TBooleanField, *TBinField, *THtmlField, *TDateField, *TDateTimeField:
		return "base", true
	case *TOne2OneField, *TOne2ManyField, *TMany2OneField, *TMany2ManyField:
		return "relation", true
	default:
		// Dynamic selections/properties and third-party Attributes overrides stay live.
		return "", false
	}
}
func (f *TField) attributeShape(field IField) (fieldAttributeShape, bool) {
	kind, ok := cachedAttributeKind(field)
	if !ok || f.depends != nil || f.selection != nil || f.uiStates != nil {
		return fieldAttributeShape{}, false
	}
	value := f.Default()
	switch value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
	default:
		return fieldAttributeShape{}, false
	}
	key := fieldAttributeShape{name: f.name, kind: kind, help: f.description, label: f.label, typeName: f.typeName, related: f.relatedPath, groups: f.permissionGroups, domain: f.domain, relationModel: f.relatedModelName, relationKey: f.relatedKeyName, digitsUsage: f.digitsUsage, minDigitsUsage: f.minDisplayDigitsUsage, groupOperator: strings.ToLower(f.groupOperator), store: f.store, manual: f.manual, readonly: f.readonly, required: f.required, sortable: f.sortable, searchable: f.searchable, indexed: f.isIndexed, translate: f.translatable, inherited: f.isInherited, size: f.size, defaultValue: value, relational: kind == "relation"}
	if len(f.digits) == 2 {
		key.hasDigits = true
		copy(key.digits[:], f.digits)
	}
	if f.minDisplayDigits != nil {
		key.hasMinDigits = true
		key.minDigits = *f.minDisplayDigits
	}
	return key, true
}
func attributeEntryBytes(key fieldAttributeShape, attrs map[string]any) int64 {
	extra := 0
	if value, ok := key.defaultValue.(string); ok {
		extra = len(value)
	}
	return int64(extra + 512 + len(attrs)*64 + len(key.name) + len(key.help) + len(key.label) + len(key.typeName) + len(key.related) + len(key.groups) + len(key.domain) + len(key.relationModel) + len(key.relationKey) + len(key.digitsUsage) + len(key.minDigitsUsage) + len(key.groupOperator))
}

// ReadOnlyAttributes may reuse immutable plain-field descriptors. Do not mutate
// its map or nested values. Attributes retains its existing caller-owned-map API.
// The context's Field must be the actual concrete field, not just its Base().
func (f *TField) ReadOnlyAttributes(ctx *TTagContext) map[string]any {
	if ctx == nil || ctx.Field == nil {
		return f.Attributes(ctx)
	}
	key, cacheable := f.attributeShape(ctx.Field)
	if !cacheable {
		return ctx.Field.Attributes(ctx)
	}
	c := fieldAttributes
	c.RLock()
	element := c.entries[key]
	generation := c.generation
	if element != nil {
		attrs := element.Value.(*fieldAttributeEntry).attrs
		c.RUnlock()
		return attrs
	}
	c.RUnlock()
	attrs := ctx.Field.Attributes(ctx)
	// Default is backed by a sync.Map and can change while the descriptor is built.
	// Never publish one default under another default's shape.
	current, ok := f.attributeShape(ctx.Field)
	if !ok || current != key || attrs["default"] != key.defaultValue {
		return attrs
	}
	if digits, ok := attrs["digits"].([]int); ok {
		attrs["digits"] = append([]int(nil), digits...)
	}
	size := attributeEntryBytes(key, attrs)
	if size > fieldAttributeMaxEntryBytes {
		return attrs
	}
	c.Lock()
	defer c.Unlock()
	if c.generation != generation {
		return attrs
	}
	if prior := c.entries[key]; prior != nil {
		return prior.Value.(*fieldAttributeEntry).attrs
	}
	for len(c.entries) >= fieldAttributeMaxEntries || c.bytes+size > fieldAttributeMaxBytes {
		c.removeFirst()
	}
	entry := &fieldAttributeEntry{key: key, attrs: attrs, bytes: size}
	c.entries[key] = c.order.PushBack(entry)
	c.bytes += size
	return attrs
}
