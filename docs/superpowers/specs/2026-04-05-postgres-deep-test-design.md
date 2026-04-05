# PostgreSQL Deep Test Design

**Date**: 2026-04-05  
**File**: `test/postgres_test.go`  
**Goal**: Implement comprehensive deep tests for all ORM interfaces against PostgreSQL, using hardcoded default connection config, and fix the transaction rollback bug where records created outside the transaction boundary persist after rollback.

---

## Connection Config (Hardcoded Defaults)

```go
&orm.TDataSource{
    DbType:   "postgres",
    Host:     "localhost",
    Port:     "5432",
    UserName: "postgres",
    Password: "postgres",
    DbName:   "test_orm",
    SSLMode:  "disable",
}
```

No environment variables. Test runs standalone via `go test -run TestPostgresDeep ./test/`.

---

## Transaction Bug: Root Cause & Fix

### Bug Description

When creating records inside a transaction session `ss`, calling `model.Records().Create(...)` creates a **new independent session** with `IsAutoCommit = true`. This session executes on an arbitrary connection from the pool, completely outside `ss.tx`. When `ss.Rollback()` is called, it only rolls back operations on `ss.tx` — the auto-committed records persist in the database.

### Affected Code

**File**: `test/test_create.go:31`

```go
// BUG: model.Records() creates a new session outside ss.tx
companyId, err := model.Records().Create(company_data, isClassic)

// FIX: use model.Tx(ss) to bind the operation to ss.tx
companyId, err := model.Tx(ss).Create(company_data, isClassic)
```

### Fix Verification

After the fix, a test that creates records, then rolls back, must confirm zero residual records via `model.Records().Where("name = ?", ...).Count()`.

---

## Architecture

```
postgres_test.go
└── TestPostgresDeep(t *testing.T)
    ├── Hardcoded TDataSource (localhost:5432, postgres/postgres, test_orm, sslmode=disable)
    ├── NewTest(t) → ORM init + SyncModel
    ├── defer: recover panic, log stack
    │
    ├── t.Run("1_Init")              → connection + schema verification
    ├── t.Run("2_Create")            → basic + edge cases + ON CONFLICT + data types
    ├── t.Run("3_Query")             → Read/Search/Count/Limit + struct mapping
    ├── t.Run("4_Conditions")        → Where/And/Or/In/NotIn/Domain
    ├── t.Run("5_Write")             → update by ID / by condition
    ├── t.Run("6_Transaction")       → rollback + commit + partial-failure rollback
    └── t.Run("7_Delete")            → single + batch delete
```

Each top-level `t.Run` contains fine-grained `t.Run` subtests for precise failure location.

---

## Step-by-Step Test Specification

### Step 1 — Init

- Verify connection: `orm.IsExist(dbName)` must return true
- Verify `SyncModel` creates tables: `partner_model`, `company_model`, `user_model`
- Verify `OnBuildFields` runs correctly:
  - `full_name` computed field exists on `user_model`
  - `title` field has a default compute function attached
  - `help` field has help text attached
- Drop all tables and re-sync to ensure clean state for subsequent steps

### Step 2 — Create

Subtests:

- **basic/struct**: Create `UserModel` struct, verify non-nil ID returned
- **basic/map**: Create with `map[string]any`, verify non-nil ID
- **data_types**: Create `UserModel` with all field types populated (int=42, bool=true, float=3.14, text="long text", binary=[]byte{1,2,3}, selection="person"), read back, compare each field value
- **edge/required_missing**: Create without `Name` (required field) → must return error
- **edge/duplicate_unique**: Create two records with identical `Name` → second must return error
- **edge/invalid_field**: Create with `map[string]any{"wrong_field": "x", "name": "valid"}` → must succeed (unknown fields silently ignored)
- **onconflict/do_nothing**: Insert duplicate with `OnConflict{DoNothing: true}` → no error, original record unchanged
- **onconflict/do_update**: Insert duplicate with `OnConflict{DoUpdates: []string{"name"}}` → record updated
- **onconflict/update_all**: Insert with `OnConflict{UpdateAll: true}` → all non-conflict fields updated

### Step 3 — Query

Subtests:

- **read/all**: `model.Records().Read()` returns non-empty dataset
- **read/select_fields**: `model.Records().Select("id","name").Read()` returns dataset with only requested fields
- **read/as_struct**: `ds.Record().AsStruct(u)` populates all fields correctly, `u.Id > 0`
- **search/all**: `Search()` returns correct ID list and total count matching
- **search/where**: `Where("id=?", id).Search()` returns exactly 1 result
- **search/limit**: `Limit(3).Search()` returns at most 3 IDs
- **count/all**: `Count()` returns value >= number of created records
- **count/where**: `Where("id>?",0).Count()` equals `Count()` all
- **limit/basic**: `Limit(3).Read()` returns at most 3 records
- **limit/offset**: `Limit(3,1).Read()` skips first record (when total > 3)

### Step 4 — Conditions

Subtests:

- **where/exact**: `Where("id=?", id)` returns single matching record
- **where/chained**: `Where("id=?",id).Where("id>?",0)` returns same record
- **and/domain_node**: Multiple `AND` conditions via `domain.NewDomainNode` returns non-empty dataset
- **or/two_ids**: `Where("id=?",id1).Or("id=?",id2).Read()` returns both records
- **in/values**: `In("title","Admin")` with matching records returns non-empty
- **notin/values**: `NotIn("name","NonExistent")` returns all records
- **domain/gt**: `Domain("[('id', '>', 0)]")` returns all records
- **domain/in**: `Domain("[('id', 'in', [id1,id2])]")` returns at most 2 records

### Step 5 — Write

Subtests:

- **write/by_ids_map**: `Ids(ids...).Write(map)` returns effect == len(ids)
- **write/by_id_struct**: `Ids(id).Write(struct)` returns effect == 1, read back verifies update
- **write/verify_update**: After write, `Read()` confirms new field value is persisted

### Step 6 — Transaction (Primary Focus)

Subtests:

- **rollback/single**: Create 1 record in `ss`, rollback, verify 0 residual records with that name
- **commit/single**: Create 1 record in `ss`, commit, verify record exists with Count==1
- **rollback/multi_partial_failure**:
  1. `ss.Begin()`
  2. Create records 0–4 with unique names in a loop using `model.Tx(ss).Create(...)`
  3. On iteration 2, attempt to insert duplicate `Name` → triggers unique constraint error
  4. `ss.Rollback(err)`
  5. Verify Count of all 5 names == 0 (none persisted)
- **rollback/related_records**:
  1. `ss.Begin()`
  2. Create `PartnerModel` via `ss` (creates row in `partner_model`)
  3. Create `CompanyModel` via `ss` referencing above partner
  4. `ss.Rollback(nil)`
  5. Verify both `partner_model` and `company_model` have 0 residual records
- **visibility/in_tx**: After `Begin()` + `Create()`, before `Commit()`, verify the new record IS visible within the same transaction session (using same `ss`)
- **nested_error/propagation**: If inner operation fails, outer `Rollback` cleans everything

### Step 7 — Delete

Subtests:

- **delete/single**: `Ids(id).Delete()` returns effect==1, Count decreases by 1
- **delete/batch**: `Ids(ids...).Delete()` returns effect==len(ids), Count decreases by len(ids)
- **delete/nonexistent**: `Ids(999999).Delete()` returns effect==0, no error

---

## ORM Features to Exercise

| Feature | Covered In |
|---|---|
| `OnBuildFields` / computed fields | Step 1, Step 2/data_types |
| `BeforeSetup` / `AfterSetup` | Step 1 (SyncModel) |
| `one2one` / `PartnerModel` relate | Step 2/basic, Step 6/related_records |
| `many2one` / `CompanyId` | Step 2/basic |
| `ON CONFLICT` (Postgres-specific) | Step 2/onconflict/* |
| `RETURNING` (Postgres-specific) | Step 2 (all creates, via `generate_insert`) |
| All SQL data types | Step 2/data_types |
| Transaction isolation | Step 6/visibility |
| Partial rollback | Step 6/rollback/multi_partial_failure |

---

## Bug Fix Location

| File | Line | Change |
|---|---|---|
| `test/test_create.go` | 31 | `model.Records().Create(...)` → `model.Tx(ss).Create(...)` |

The `Create` test's session (`ss`) must be properly propagated to all create calls within the transaction boundary. After this fix, the multi-record partial-failure rollback test in Step 6 must pass.

---

## Out of Scope

- Many2many relation table creation (requires manual intermediate table setup — tracked separately)
- MySQL / SQLite dialect tests (covered by `orm_test.go`)
- Concurrency / race condition tests (separate concern)
