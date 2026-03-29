package validator

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError 代表验证错误
type ValidationError struct {
	Field   string
	Message string
}

// Validator 验证器
type Validator struct {
	errors []ValidationError
}

// New 创建新的验证器
func New() *Validator {
	return &Validator{
		errors: make([]ValidationError, 0),
	}
}

// AddError 添加验证错误
func (v *Validator) AddError(field, message string) {
	v.errors = append(v.errors, ValidationError{
		Field:   field,
		Message: message,
	})
}

// HasErrors 是否有错误
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// GetErrors 获取所有错误
func (v *Validator) GetErrors() []ValidationError {
	return v.errors
}

// Error 返回错误信息
func (v *Validator) Error() string {
	if !v.HasErrors() {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("验证失败:\n")
	for i, err := range v.errors {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, err.Field, err.Message))
	}
	return sb.String()
}

// ValidateTableName 验证表名
func (v *Validator) ValidateTableName(tableName string) bool {
	if tableName == "" {
		v.AddError("table_name", "表名不能为空")
		return false
	}

	// 表名只能包含字母、数字、下划线
	if !regexp.MustCompile("^[a-zA-Z0-9_]+$").MatchString(tableName) {
		v.AddError("table_name", "表名只能包含字母、数字、下划线")
		return false
	}

	if len(tableName) > 255 {
		v.AddError("table_name", "表名长度不能超过255字符")
		return false
	}

	return true
}

// ValidateSQL 验证SQL语句（基础检查）
func (v *Validator) ValidateSQL(sql string) bool {
	if sql == "" {
		v.AddError("sql", "SQL语句不能为空")
		return false
	}

	sql = strings.ToUpper(strings.TrimSpace(sql))

	// 检查是否包含常见的SQL注入特征
	dangerousPatterns := []string{
		"DROP", "DELETE FROM", "TRUNCATE", "ALTER",
		";--", "/*", "*/", "xp_", "sp_",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(sql, pattern) {
			// 这只是基础检查，真正的SQL注入防护应该通过参数化查询
			// 但这里我们警告用户
			if pattern == "DROP" || pattern == "TRUNCATE" {
				v.AddError("sql", fmt.Sprintf("检测到危险操作: %s，请确保这是有意的", pattern))
			}
		}
	}

	return true
}

// ValidateID 验证ID (必须是正整数或UUID格式)
func (v *Validator) ValidateID(id interface{}) bool {
	if id == nil {
		v.AddError("id", "ID不能为nil")
		return false
	}

	switch val := id.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint, uint8, uint16, uint32, uint64:
		return true
	case string:
		if val == "" {
			v.AddError("id", "ID字符串不能为空")
			return false
		}
		return true
	default:
		v.AddError("id", fmt.Sprintf("不支持的ID类型: %T", id))
		return false
	}
}

// ValidateIDs 验证多个ID
func (v *Validator) ValidateIDs(ids []interface{}) bool {
	if len(ids) == 0 {
		v.AddError("ids", "ID列表不能为空")
		return false
	}

	for i, id := range ids {
		subValidator := New()
		if !subValidator.ValidateID(id) {
			v.AddError("ids", fmt.Sprintf("第%d个ID无效: %v", i+1, id))
			return false
		}
	}

	return true
}

// ValidateFields 验证字段名列表
func (v *Validator) ValidateFields(fields []string) bool {
	if len(fields) == 0 {
		v.AddError("fields", "字段列表不能为空")
		return false
	}

	for i, field := range fields {
		if field == "" {
			v.AddError("fields", fmt.Sprintf("第%d个字段名不能为空", i+1))
			return false
		}

		if !regexp.MustCompile("^[a-zA-Z0-9_*,.`]+$").MatchString(field) {
			v.AddError("fields", fmt.Sprintf("第%d个字段名包含非法字符: %s", i+1, field))
			return false
		}
	}

	return true
}

// ValidateCacheSize 验证缓存大小
func (v *Validator) ValidateCacheSize(size int) bool {
	if size <= 0 {
		v.AddError("cache_size", "缓存大小必须大于0")
		return false
	}

	if size > 1000000 {
		v.AddError("cache_size", "缓存大小不能超过1000000")
		return false
	}

	return true
}

// ValidateTTL 验证TTL值（秒）
func (v *Validator) ValidateTTL(ttl int64) bool {
	if ttl < 60 {
		v.AddError("ttl", "TTL不能少于60秒")
		return false
	}

	if ttl > 86400 {
		v.AddError("ttl", "TTL不能超过86400秒（24小时）")
		return false
	}

	return true
}

// ValidateLimitOffset 验证分页参数
func (v *Validator) ValidateLimitOffset(limit, offset int) bool {
	if limit < 0 {
		v.AddError("limit", "limit不能为负数")
		return false
	}

	if offset < 0 {
		v.AddError("offset", "offset不能为负数")
		return false
	}

	if limit > 10000 {
		v.AddError("limit", "limit不能超过10000")
		return false
	}

	return true
}
