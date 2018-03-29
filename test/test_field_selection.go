package test

import (
	//	"fmt"
	"testing"
	"vectors/orm"
)

func FieldSelection(o *orm.TOrm, t *testing.T) {
	err := o.SyncModel("test", new(Model1))
	if err != nil {
		panic(err.Error())
	}

	model, _ := o.GetModel("sys.action")
	if model == nil {
		panic("Syncmodel error! didnot found model!")
	}

	field := model.FieldByName("type")
	t.Log(field)
	field = model.FieldByName("lang")
	t.Log(field.(*orm.TSelectionField).GetAttributes(&orm.TFieldContext{Model: model, Field: field}))
}
