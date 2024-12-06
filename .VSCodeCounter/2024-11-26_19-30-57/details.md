# Details

Date : 2024-11-26 19:30:57

Directory /Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm

Total : 108 files,  15474 codes, 3990 comments, 3000 blanks, all 22464 lines

[Summary](results.md) / Details / [Diff Summary](diff.md) / [Diff Details](diff-details.md)

## Files
| filename | language | code | comment | blank | total |
| :--- | :--- | ---: | ---: | ---: | ---: |
| [orm/README.md](/orm/README.md) | Markdown | 150 | 0 | 25 | 175 |
| [orm/benchmark.go](/orm/benchmark.go) | Go | 20 | 116 | 6 | 142 |
| [orm/cacher/cacher.go](/orm/cacher/cacher.go) | Go | 164 | 62 | 40 | 266 |
| [orm/config.go](/orm/config.go) | Go | 48 | 0 | 10 | 58 |
| [orm/const.go](/orm/const.go) | Go | 10 | 0 | 3 | 13 |
| [orm/core/context.go](/orm/core/context.go) | Go | 60 | 8 | 10 | 78 |
| [orm/core/core.go](/orm/core/core.go) | Go | 15 | 3 | 5 | 23 |
| [orm/core/db.go](/orm/core/db.go) | Go | 230 | 35 | 52 | 317 |
| [orm/core/error.go](/orm/core/error.go) | Go | 6 | 2 | 3 | 11 |
| [orm/core/log.go](/orm/core/log.go) | Go | 3 | 0 | 3 | 6 |
| [orm/core/mapper.go](/orm/core/mapper.go) | Go | 185 | 10 | 38 | 233 |
| [orm/core/row.go](/orm/core/row.go) | Go | 269 | 22 | 52 | 343 |
| [orm/core/scan.go](/orm/core/scan.go) | Go | 52 | 6 | 9 | 67 |
| [orm/core/stmt.go](/orm/core/stmt.go) | Go | 156 | 20 | 41 | 217 |
| [orm/core/tx.go](/orm/core/tx.go) | Go | 178 | 26 | 29 | 233 |
| [orm/core/ver.go](/orm/core/ver.go) | Go | 6 | 1 | 2 | 9 |
| [orm/dataset.go](/orm/dataset.go) | Go | 7 | 0 | 3 | 10 |
| [orm/datasource.go](/orm/datasource.go) | Go | 82 | 2 | 15 | 99 |
| [orm/dialect.go](/orm/dialect.go) | Go | 305 | 60 | 72 | 437 |
| [orm/dialect/funcs.go](/orm/dialect/funcs.go) | Go | 5 | 0 | 2 | 7 |
| [orm/dialect/quoter.go](/orm/dialect/quoter.go) | Go | 187 | 25 | 30 | 242 |
| [orm/dialect_mysql.go](/orm/dialect_mysql.go) | Go | 737 | 27 | 82 | 846 |
| [orm/dialect_postgres.go](/orm/dialect_postgres.go) | Go | 1,358 | 77 | 118 | 1,553 |
| [orm/domain/config.json](/orm/domain/config.json) | JSON | 12 | 0 | 0 | 12 |
| [orm/domain/domain.go](/orm/domain/domain.go) | Go | 450 | 125 | 98 | 673 |
| [orm/domain/domain_test.go](/orm/domain/domain_test.go) | Go | 99 | 0 | 21 | 120 |
| [orm/domain/parser.go](/orm/domain/parser.go) | Go | 227 | 83 | 49 | 359 |
| [orm/domain/parser_test.go](/orm/domain/parser_test.go) | Go | 72 | 1 | 12 | 85 |
| [orm/driver.go](/orm/driver.go) | Go | 21 | 0 | 7 | 28 |
| [orm/driver_mysql.go](/orm/driver_mysql.go) | Go | 116 | 4 | 11 | 131 |
| [orm/driver_postgres.go](/orm/driver_postgres.go) | Go | 95 | 4 | 23 | 122 |
| [orm/error.go](/orm/error.go) | Go | 42 | 1 | 10 | 53 |
| [orm/errors/errors.go](/orm/errors/errors.go) | Go | 21 | 0 | 6 | 27 |
| [orm/expr.go](/orm/expr.go) | Go | 637 | 470 | 147 | 1,254 |
| [orm/expr_leaf.go](/orm/expr_leaf.go) | Go | 108 | 76 | 26 | 210 |
| [orm/expr_test.go](/orm/expr_test.go) | Go | 26 | 0 | 8 | 34 |
| [orm/field.go](/orm/field.go) | Go | 542 | 168 | 105 | 815 |
| [orm/field_chars.go](/orm/field_chars.go) | Go | 74 | 2 | 16 | 92 |
| [orm/field_config.go](/orm/field_config.go) | Go | 29 | 1 | 7 | 37 |
| [orm/field_id.go](/orm/field_id.go) | Go | 59 | 27 | 13 | 99 |
| [orm/field_name.go](/orm/field_name.go) | Go | 27 | 2 | 8 | 37 |
| [orm/field_number.go](/orm/field_number.go) | Go | 66 | 1 | 15 | 82 |
| [orm/field_object.go](/orm/field_object.go) | Go | 27 | 4 | 7 | 38 |
| [orm/field_relational.go](/orm/field_relational.go) | Go | 473 | 184 | 103 | 760 |
| [orm/field_selection.go](/orm/field_selection.go) | Go | 111 | 38 | 24 | 173 |
| [orm/field_time.go](/orm/field_time.go) | Go | 31 | 0 | 8 | 39 |
| [orm/go.mod](/orm/go.mod) | Go Module File | 34 | 0 | 4 | 38 |
| [orm/go.sum](/orm/go.sum) | Go Checksum File | 514 | 0 | 1 | 515 |
| [orm/index.go](/orm/index.go) | Go | 79 | 24 | 15 | 118 |
| [orm/model.go](/orm/model.go) | Go | 435 | 218 | 106 | 759 |
| [orm/model_builder.go](/orm/model_builder.go) | Go | 244 | 19 | 54 | 317 |
| [orm/model_request.go](/orm/model_request.go) | Go | 613 | 153 | 139 | 905 |
| [orm/mothod.go](/orm/mothod.go) | Go | 67 | 21 | 17 | 105 |
| [orm/orm.go](/orm/orm.go) | Go | 625 | 156 | 135 | 916 |
| [orm/orm_test.go](/orm/orm_test.go) | Go | 165 | 33 | 17 | 215 |
| [orm/osv.go](/orm/osv.go) | Go | 373 | 160 | 84 | 617 |
| [orm/osv_test.go](/orm/osv_test.go) | Go | 1 | 65 | 3 | 69 |
| [orm/query.go](/orm/query.go) | Go | 159 | 136 | 32 | 327 |
| [orm/session.go](/orm/session.go) | Go | 432 | 99 | 90 | 621 |
| [orm/session_crwd.go](/orm/session_crwd.go) | Go | 1,027 | 343 | 208 | 1,578 |
| [orm/session_expr.go](/orm/session_expr.go) | Go | 109 | 79 | 29 | 217 |
| [orm/session_query.go](/orm/session_query.go) | Go | 273 | 44 | 61 | 378 |
| [orm/session_tx.go](/orm/session_tx.go) | Go | 35 | 16 | 10 | 61 |
| [orm/sql.go](/orm/sql.go) | Go | 42 | 5 | 9 | 56 |
| [orm/statement.go](/orm/statement.go) | Go | 522 | 290 | 103 | 915 |
| [orm/tag.go](/orm/tag.go) | Go | 594 | 111 | 129 | 834 |
| [orm/test/model.go](/orm/test/model.go) | Go | 95 | 11 | 16 | 122 |
| [orm/test/postgres/0_delete_test.go](/orm/test/postgres/0_delete_test.go) | Go | 8 | 0 | 4 | 12 |
| [orm/test/postgres/0_read_test.go](/orm/test/postgres/0_read_test.go) | Go | 8 | 2 | 4 | 14 |
| [orm/test/postgres/0_write_test.go](/orm/test/postgres/0_write_test.go) | Go | 8 | 0 | 4 | 12 |
| [orm/test/postgres/1_and_test.go](/orm/test/postgres/1_and_test.go) | Go | 6 | 2 | 3 | 11 |
| [orm/test/postgres/1_in_test.go](/orm/test/postgres/1_in_test.go) | Go | 9 | 0 | 4 | 13 |
| [orm/test/postgres/1_notin_test.go](/orm/test/postgres/1_notin_test.go) | Go | 8 | 0 | 4 | 12 |
| [orm/test/postgres/1_or_test.go](/orm/test/postgres/1_or_test.go) | Go | 8 | 0 | 4 | 12 |
| [orm/test/postgres/1_where_test.go](/orm/test/postgres/1_where_test.go) | Go | 6 | 2 | 3 | 11 |
| [orm/test/postgres/3_field_selection_test.go](/orm/test/postgres/3_field_selection_test.go) | Go | 7 | 1 | 4 | 12 |
| [orm/test/postgres/3_filed_relational_test.go](/orm/test/postgres/3_filed_relational_test.go) | Go | 6 | 1 | 3 | 10 |
| [orm/test/postgres/config.json](/orm/test/postgres/config.json) | JSON | 16 | 0 | 0 | 16 |
| [orm/test/postgres/conn_test.go](/orm/test/postgres/conn_test.go) | Go | 8 | 0 | 4 | 12 |
| [orm/test/postgres/create_test.go](/orm/test/postgres/create_test.go) | Go | 21 | 0 | 7 | 28 |
| [orm/test/postgres/postgres.go](/orm/test/postgres/postgres.go) | Go | 15 | 1 | 4 | 20 |
| [orm/test/postgres/postgres_test.go](/orm/test/postgres/postgres_test.go) | Go | 12 | 6 | 6 | 24 |
| [orm/test/test.go](/orm/test/test.go) | Go | 116 | 71 | 31 | 218 |
| [orm/test/test_and.go](/orm/test/test_and.go) | Go | 27 | 1 | 6 | 34 |
| [orm/test/test_conn.go](/orm/test/test_conn.go) | Go | 8 | 0 | 3 | 11 |
| [orm/test/test_count.go](/orm/test/test_count.go) | Go | 19 | 1 | 6 | 26 |
| [orm/test/test_create.go](/orm/test/test_create.go) | Go | 153 | 4 | 43 | 200 |
| [orm/test/test_delete.go](/orm/test/test_delete.go) | Go | 11 | 0 | 6 | 17 |
| [orm/test/test_domain.go](/orm/test/test_domain.go) | Go | 20 | 1 | 7 | 28 |
| [orm/test/test_drop.go](/orm/test/test_drop.go) | Go | 1 | 0 | 1 | 2 |
| [orm/test/test_dump.go](/orm/test/test_dump.go) | Go | 6 | 0 | 4 | 10 |
| [orm/test/test_field_selection.go](/orm/test/test_field_selection.go) | Go | 6 | 16 | 3 | 25 |
| [orm/test/test_filed_relational.go](/orm/test/test_filed_relational.go) | Go | 7 | 66 | 4 | 77 |
| [orm/test/test_in.go](/orm/test/test_in.go) | Go | 16 | 0 | 6 | 22 |
| [orm/test/test_limit.go](/orm/test/test_limit.go) | Go | 6 | 1 | 4 | 11 |
| [orm/test/test_method.go](/orm/test/test_method.go) | Go | 9 | 0 | 3 | 12 |
| [orm/test/test_notin.go](/orm/test/test_notin.go) | Go | 6 | 29 | 3 | 38 |
| [orm/test/test_or.go](/orm/test/test_or.go) | Go | 6 | 28 | 3 | 37 |
| [orm/test/test_read.go](/orm/test/test_read.go) | Go | 46 | 2 | 12 | 60 |
| [orm/test/test_search.go](/orm/test/test_search.go) | Go | 17 | 0 | 6 | 23 |
| [orm/test/test_sum.go](/orm/test/test_sum.go) | Go | 6 | 0 | 4 | 10 |
| [orm/test/test_sync.go](/orm/test/test_sync.go) | Go | 89 | 2 | 12 | 103 |
| [orm/test/test_table_name.go](/orm/test/test_table_name.go) | Go | 6 | 1 | 3 | 10 |
| [orm/test/test_tag.go](/orm/test/test_tag.go) | Go | 35 | 0 | 25 | 60 |
| [orm/test/test_where.go](/orm/test/test_where.go) | Go | 35 | 1 | 10 | 46 |
| [orm/test/test_write.go](/orm/test/test_write.go) | Go | 76 | 6 | 24 | 106 |
| [orm/type.go](/orm/type.go) | Go | 337 | 27 | 46 | 410 |
| [orm/utils.go](/orm/utils.go) | Go | 228 | 42 | 31 | 301 |

[Summary](results.md) / Details / [Diff Summary](diff.md) / [Diff Details](diff-details.md)