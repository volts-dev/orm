package orm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/volts-dev/dataset"
)

// NormalizeValues converts arbitrary input values into a *dataset.TDataSet.
//
// Supported inputs:
//   - *dataset.TDataSet : returned as-is
//   - map[string]any    : wrapped in a new TDataSet
//   - map[string]string : keys/values copied into map[string]any then wrapped
//   - struct / *struct  : converted via StructToMap (requires non-nil model)
//
// model is only consulted for the struct path; it is used to look up the ORM
// tag identifier, the field definitions and time formatting. Pass nil when
// you know the value is not a struct.
func NormalizeValues(values any, model IModel) (*dataset.TDataSet, error) {
	if values == nil {
		return nil, fmt.Errorf("must submit the values for update")
	}
	switch v := values.(type) {
	case *dataset.TDataSet:
		return v, nil
	case map[string]any:
		return dataset.NewDataSet(dataset.WithData(v)), nil
	case map[string]string:
		m := make(map[string]any, len(v))
		for k, val := range v {
			m[k] = val
		}
		return dataset.NewDataSet(dataset.WithData(m)), nil
	}

	rv := reflect.ValueOf(values)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("unsupported values type: %T", values)
	}
	if model == nil {
		return nil, fmt.Errorf("orm.NormalizeValues: struct input requires a non-nil model")
	}
	return dataset.NewDataSet(dataset.WithData(StructToMap(values, model, nil))), nil
}

// structFieldInfo holds the reflection metadata for one struct field that
// does not change between calls (name, index, kind flags). Per-call
// context-dependent checks (model field existence, SQLType) still run each time.
type structFieldInfo struct {
	reflectIdx int    // index for reflect.Type.Field(i) / reflect.Value.Field(i)
	ormName    string // final ORM field name after fmtFieldName + tag "name" override
	isExtends  bool   // tag has "extends" or "relate" — recurse into nested struct
	isTime     bool   // field type is ConvertibleTo(TimeType)
	skip       bool   // unexported or tag == "-"
}

// structCacheKey is a compound key for structInfoCache.
// Includes both the reflect.Type and the FieldIdentifier tag name,
// so that different TOrm instances with different FieldIdentifiers
// don't share the same cache entry.
type structCacheKey struct {
	t               reflect.Type
	fieldIdentifier string
}

// structInfoCache maps structCacheKey → []structFieldInfo.
// Populated lazily; struct field shapes never change, so entries are never invalidated.
var structInfoCache sync.Map

// StructToMap converts src (struct or *struct) to map[string]any using model
// metadata. fieldFilter, if non-empty, filters orm-named fields: only those
// with a true value in the map are kept (mirrors the Statement.Fields semantics).
//
// The conversion needs model for:
//   - model.Orm().Config().FieldIdentifier — struct tag name
//   - model.Obj().GetFieldByName           — field existence + SQLType
//   - model.Orm().FormatTime               — time field formatting
func StructToMap(src any, model IModel, fieldFilter map[string]bool) (res_map map[string]any) {
	v := reflect.ValueOf(src)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		log.Warn("StructToMap: not a struct")
		return nil
	}
	if model == nil {
		log.Warn("StructToMap: model is nil")
		return nil
	}

	orm := model.Orm()
	fieldIdentifier := orm.Config().FieldIdentifier
	vType := v.Type()

	// load or build cache entry
	var infos []structFieldInfo
	cacheKey := structCacheKey{t: vType, fieldIdentifier: fieldIdentifier}
	if cached, ok := structInfoCache.Load(cacheKey); ok {
		infos = cached.([]structFieldInfo)
	} else {
		infos = make([]structFieldInfo, 0, vType.NumField())
		for i := 0; i < vType.NumField(); i++ {
			sf := vType.Field(i)
			info := structFieldInfo{reflectIdx: i}

			// unexported
			if sf.PkgPath != "" {
				info.skip = true
				infos = append(infos, info)
				continue
			}

			tag := sf.Tag.Get(fieldIdentifier)
			if tag == "-" {
				info.skip = true
				infos = append(infos, info)
				continue
			}

			// default ORM name
			info.ormName = fmtFieldName(sf.Name)

			// parse tag for name override and extends/relate
			for _, part := range splitTag(tag) {
				parsed := parseTag(part)
				switch strings.ToLower(parsed[0]) {
				case "name":
					if len(parsed) > 1 {
						info.ormName = fmtFieldName(parsed[1])
					}
				case "extends", "relate":
					info.isExtends = true
				}
			}

			// time detection (type-level, not value-level)
			ft := sf.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			info.isTime = ft.ConvertibleTo(TimeType)

			infos = append(infos, info)
		}
		structInfoCache.Store(cacheKey, infos)
	}

	res_map = make(map[string]any)
	lToOmitFields := len(fieldFilter) > 0

	for _, info := range infos {
		if info.skip {
			continue
		}

		// per-call: fieldFilter
		if lToOmitFields {
			if b, ok := fieldFilter[info.ormName]; ok && !b {
				continue
			}
		}

		fv := v.Field(info.reflectIdx)

		// extends/relate: recurse
		if info.isExtends {
			if (fv.Kind() == reflect.Ptr && fv.Elem().Kind() == reflect.Struct) ||
				fv.Kind() == reflect.Struct {
				for col, val := range StructToMap(fv.Interface(), model, fieldFilter) {
					res_map[col] = val
				}
			}
			continue
		}

		// per-call: model field existence check
		lCol := model.Obj().GetFieldByName(info.ormName)
		if lCol == nil {
			continue
		}

		var lValue any
		if info.isTime {
			timeFv := fv
			if timeFv.Kind() == reflect.Ptr {
				if timeFv.IsNil() {
					continue // nil *time.Time — skip this field
				}
				timeFv = timeFv.Elem()
			}
			t := timeFv.Convert(TimeType).Interface().(time.Time)
			lValue = orm.FormatTime(lCol.Base().SQLType().Name, t)
		} else {
			switch fv.Kind() {
			case reflect.Struct:
				// non-time struct: JSON or blob
				col := lCol.Base()
				if col.SQLType().IsJson() {
					if col.SQLType().IsText() {
						if b, err := json.Marshal(fv.Interface()); err == nil {
							lValue = string(b)
						} else {
							log.Errf("IsJson/Text", err)
							continue
						}
					} else if col.SQLType().IsBlob() {
						if b, err := json.Marshal(fv.Interface()); err == nil {
							lValue = b
						} else {
							log.Errf("IsJson/Blob", err)
							continue
						}
					} else {
						log.Err("unhandled struct field type", info.ormName)
					}
				}
			default:
				lValue = fv.Interface()
			}
		}

		if lValue == nil && fv.IsValid() {
			lValue = fv.Interface()
		}
		res_map[info.ormName] = lValue
	}

	return
}
