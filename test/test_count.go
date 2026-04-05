package test

func (self *Testchain) Count() *Testchain {
	self.PrintSubject("Count")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	// count all records
	total, err := model.Records().Count()
	if err != nil {
		self.Fatal(err)
	}
	if total < 0 {
		self.Fatalf("Count() returned negative value: %d", total)
	}

	// count with where condition
	total2, err := model.Records().Where("id>?", 0).Count()
	if err != nil {
		self.Fatal(err)
	}
	if total2 != total {
		self.Fatalf("Count with WHERE id>0 mismatch: %d vs %d", total2, total)
	}

	return self
}
