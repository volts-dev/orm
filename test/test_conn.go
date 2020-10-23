package test

func (self *Testchain) Conn() *Testchain {
	self.PrintSubject("Connection")
	if !self.Orm.IsExist(TEST_DB_NAME) {
		self.Fatalf("IsExist failed!")
	}

	return self
}
