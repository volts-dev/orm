package core

// Version represents a database version
type Version struct {
	Number  string // the version number which could be compared
	Level   string
	Edition string
}
