package test

import "testing"

func TestGetModelReusesOnlyIdenticalInheritanceMetadata(t *testing.T) {
	o := newOne2OneOrm(t)
	defer o.Close()
	first, err := o.GetModel("company_model")
	if err != nil {
		t.Fatal(err)
	}
	mapping := first.Obj().GetRelatedFieldByName("homepage")
	if mapping == nil {
		t.Fatal("missing inherited mapping")
	}
	second, err := o.GetModel("company_model")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("request model wrappers became shared")
	}
	if second.Obj().GetRelatedFieldByName("homepage") != mapping {
		t.Fatal("identical inherited mapping rebuilt on GetModel")
	}
}
