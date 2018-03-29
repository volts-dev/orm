package orm

import (
	"fmt"
	"testing"
	"vectors/utils"
)

var (
	show_items bool = true
)

func TestInsertLexing(t *testing.T) {
	itemNames := map[ItemType]string{
		ItemError:      "error",
		ItemEOF:        "EOF",
		ItemKeyword:    "keyword",
		ItemOperator:   "operator",
		ItemIdentifier: "identifier",
		ItemLeftParen:  "left_paren",
		ItemNumber:     "number",
		ItemRightParen: "right_paren",
		ItemWhitespace: "space",
		ItemString:     "string",
		//ItemComment:        "comment",
		//ItemStatementStart: "statement_start",
		ItemStetementEnd: "statement_end",
		ItemValueHolder:  "ItemValueHolder",
	}

	fn_print_item := func(i Item) string {
		if itemNames[i.Type] != "" {
			return fmt.Sprintf(">> %q('%q')", itemNames[i.Type], i.Val)
		}

		return ""
	}

	/*	//query := "select * from aa WHERE passport='create' and password='create' and lower(id) = lower(15) or gg in (?)"
		view_id := "1"
		model := ""
		query := "inherit_id=" + view_id + " and model='" + model + "' and mode='extension' and active=true"
		//query = "rec_uid=" + strconv.FormatInt(13, 10)
		query = `(inherit_id=1 and model='333') or (mode='extension' and active=true) and ("fdsf" in ("a","b"))`
		//query = `lang ilike '?'`

		domain := Query2Domain(query)
		utils.PrintStringList(domain)
		fmt.Println("result:", domain.Count(), domain.Item(0).String(0), domain.Item(0).String(1), StringList2Domain(domain))
	*/
	query := `[('model', '=','%s'),('type', '=', '%s'), ('mode', '=', 'primary')]`
	query = `(inherit_id=1 and model='333') or (mode='extension' and active=true) and ("fdsf" in ("a","b"))`
	parser := NewDomainParser(query)

	if show_items {
		for _, item := range parser.items {
			fmt.Println(fn_print_item(item))
		}
	}

	domain, err := _parse_query(parser, 0)
	if err != nil {

	}
	// 确保Domain为List形态
	for {
		if domain.Count() == 1 {
			domain = domain.Item(0)
			continue
		}

		break
	}

	utils.PrintStringList(domain)
	fmt.Println("result:", domain.Count(), domain.Item(0).String(0), domain.Item(0).String(1), StringList2Domain(domain))

}
