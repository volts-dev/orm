package orm

import "reflect"

// A relation reload still resolves every current parent and field. Preserve an
// already identical mapping instead of allocating and publishing a replacement
// on every GetModel. This stores no model wrapper or request/session context.
func (obj *TModelObject) refreshRelatedField(name, table, link string, field IField, root string) {
	obj.relatedFieldsLock.Lock()
	defer obj.relatedFieldsLock.Unlock()
	old := obj.relatedFields[name]
	if old != nil && old.name == name && old.RelatedTableName == table && old.RelatedFieldName == link && old.RelatedRootModel == root &&
		(field == nil || reflect.TypeOf(field).Kind() == reflect.Pointer) && old.RelatedField == field {
		return
	}
	// Third-party value fields retain the original replacement path. Only use
	// pointer identity; never compare arbitrary interfaces or mutate old mappings.
	obj.relatedFields[name] = NewRelatedField(name, table, link, field, root)
}
