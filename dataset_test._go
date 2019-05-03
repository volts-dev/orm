package orm

import (
	//"fmt"
	"testing"
)

func TestDataset_NewRec(t *testing.T) {
	ds := NewDataSet()
	rec := NewRecordSet()
	// 测试动态添加字段和值
	rec.GetByName("name").AsString("AAAA")
	rec.GetByName("name2").AsInterface(map[string]string{"fasd": "asdf"})
	ds.AppendRecord(rec)
	ds.classic = false
	ds.FieldByName("name3").AsString("CCCC")

	t.Log(rec.AsItfMap(), ds.Count(), ds.Record().AsItfMap())
	t.Log(ds.FieldByName("name").AsString())
	t.Log(ds.FieldByName("name2").AsInterface(), ds.Record().Fields, ds.Record()._getByName("name2", false))
	t.Log(ds.FieldByName("name3").AsString(), ds.Record().Values, ds.Record()._getByName("name3", false))

	ds.Delete()
	t.Log(rec.AsItfMap(), ds.Count(), ds.Record().AsItfMap())

}
