package orm

import (
	"testing"
)

func TestDelete(title string, t *testing.T) {
	PrintSubject(title, "Delete()")
	test_delete(test_orm, t)
}

func test_delete(o *TOrm, t *testing.T) {
	ids := test_create(test_orm, t, 2)

	user_model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatalf(err.Error())
	}

	effected, err := user_model.Records().Delete(ids...)
	if effected != 2 {
		t.Fatalf("delete failure")
	}
}
