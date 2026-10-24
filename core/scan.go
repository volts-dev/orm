// 本文件部分代码源自 xorm core（https://gitea.com/xorm/core），BSD 3-Clause 许可，
// Copyright (c) 2013 - 2015 Lunny Xiao。许可全文见仓库根目录 LICENSE-xorm。

package core

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// NullTime defines a customize type NullTime
type NullTime time.Time

var (
	_ driver.Valuer = NullTime{}
)

// Scan implements driver.Valuer
func (ns *NullTime) Scan(value any) error {
	if value == nil {
		return nil
	}
	return convertTime(ns, value)
}

// Value implements the driver Valuer interface.
func (ns NullTime) Value() (driver.Value, error) {
	if (time.Time)(ns).IsZero() {
		return nil, nil
	}
	return (time.Time)(ns).Format("2006-01-02 15:04:05"), nil
}

func convertTime(dest *NullTime, src any) error {
	// Common cases, without reflect.
	switch s := src.(type) {
	case string:
		t, err := time.Parse("2006-01-02 15:04:05", s)
		if err != nil {
			return err
		}
		*dest = NullTime(t)
		return nil
	case []uint8:
		t, err := time.Parse("2006-01-02 15:04:05", string(s))
		if err != nil {
			return err
		}
		*dest = NullTime(t)
		return nil
	case time.Time:
		*dest = NullTime(s)
		return nil
	case nil:
	default:
		return fmt.Errorf("unsupported driver -> Scan pair: %T -> %T", src, dest)
	}
	return nil
}

// EmptyScanner represents an empty scanner
type EmptyScanner struct {
}

// Scan implements
func (EmptyScanner) Scan(src any) error {
	return nil
}
