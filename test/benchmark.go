package test

import (
	//	"database/sql"
	"testing"
	"volts-dev/orm"
)

type BigStruct struct {
	Id       int64
	Name     string
	Title    string
	Age      string
	Alias    string
	NickName string
}

/*
func DoBenchDriverInsert(db *sql.DB, b *testing.B) {
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.Exec(`insert into big_struct (name, title, age, alias, nick_name)
            values ('fafdasf', 'fadfa', 'afadfsaf', 'fadfafdsafd', 'fadfafdsaf')`)
		if err != nil {
			b.Error(err)
			return
		}
	}
	b.StopTimer()
}

func DoBenchDriverFind(db *sql.DB, b *testing.B) {
	b.StopTimer()
	for i := 0; i < 50; i++ {
		_, err := db.Exec(`insert into big_struct (name, title, age, alias, nick_name)
            values ('fafdasf', 'fadfa', 'afadfsaf', 'fadfafdsafd', 'fadfafdsaf')`)
		if err != nil {
			b.Error(err)
			return
		}
	}

	b.StartTimer()
	for i := 0; i < b.N/50; i++ {
		rows, err := db.Query("select * from big_struct limit 50")
		if err != nil {
			b.Error(err)
			return
		}
		for rows.Next() {
			s := &BigStruct{}
			rows.Scan(&s.Id, &s.Name, &s.Title, &s.Age, &s.Alias, &s.NickName)
		}
	}
	b.StopTimer()
}

func DoBenchDriver(newdriver func() (*sql.DB, error), createTableSql,
	dropTableSql string, opFunc func(*sql.DB, *testing.B), t *testing.B) {
	db, err := newdriver()
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()

	_, err = db.Exec(createTableSql)
	if err != nil {
		t.Error(err)
		return
	}

	opFunc(db, t)

	_, err = db.Exec(dropTableSql)
	if err != nil {
		t.Error(err)
		return
	}
}*/

func DoBenchInsert(orm *orm.TOrm, b *testing.B) {
	b.StopTimer()
	/*	lUserData := &ResUser{Passport: "create", Password: "create", CompanyId: 1}
		//err := orm.CreateTables(bs)
		//if err != nil {
		//	b.Error(err)
		//	return
		//}

		b.StartTimer()
		model, _ := orm.GetModel("res.user")
		for i := 0; i < b.N; i++ {
			lUserData.Id = 0
			_, err := model.Records().Create(lUserData)
			if err != nil {
				b.Error(err)
				return
			}
		}*/
	b.StopTimer()
	/*	err = engine.DropTables(bs)
		if err != nil {
			b.Error(err)
			return
		}*/
}

func DoBenchFind(orm *orm.TOrm, b *testing.B) {
	b.StopTimer()
	/*lUserData := &ResUser{Passport: "create", Password: "create", CompanyId: 1}
	/*	err := engine.CreateTables(bs)
		if err != nil {
			b.Error(err)
			return
		}
	*/
	/*model, _ := orm.GetModel("res.user")
	for i := 0; i < 100; i++ {
		lUserData.Id = 0
		_, err := model.Records().Create(lUserData)
		if err != nil {
			b.Error(err)
			return
		}
	}

	b.StartTimer()
	for i := 0; i < b.N/50; i++ {
		_, err := model.Records().Limit(50).Read()
		if err != nil {
			b.Error(err)
			return
		}
	}*/
	b.StopTimer()
	/*	err = engine.DropTables(bs)
		if err != nil {
			b.Error(err)
			return
		}*/
}
