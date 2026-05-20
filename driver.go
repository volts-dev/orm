package orm

type (
	IDriver interface {
		Parse(string, string) (*TDataSource, error)
	}
)

var (
	driver_creators = make(map[string]func() IDriver)
)

func RegisterDriver(name string, driver func() IDriver) {
	if driver == nil {
		panic("Register driver creator is nil")
	}

	if _, dup := driver_creators[name]; dup {
		panic("Register called twice for driver creator" + name)
	}

	driver_creators[name] = driver
}

func QueryDriver(name string) func() IDriver {
	return driver_creators[name]
}
