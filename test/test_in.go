package test

func (self *Testchain) In() *Testchain {
	self.PrintSubject("In")

	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

	ds, err := model.Records().In("title", "Admin").Where("name=?", "Admin").Read()
	if err != nil {
		self.Fatal(err)
	}

	if ds.IsEmpty() {
		self.Fatalf("the action Read() return %d!", ds.Count())
	}

	return self
}
