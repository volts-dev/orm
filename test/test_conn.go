package test

func (self *Testchain) Conn() *Testchain {
	self.PrintSubject("Connection")
	if !self.orm.IsExist(TEST_DB_NAME) {
		self.Fatalf("IsExist failed!")
	}

	return self
}
