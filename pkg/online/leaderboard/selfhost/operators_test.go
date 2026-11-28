package selfhost

import "testing"

func TestBoardOperatorsAndMonthlyConfig(t *testing.T) {
	c := Config{Version: ConfigVersion, Boards: []Board{{Name: "season", Order: Descending, Reset: Monthly, Operator: OperatorIncrement, MetadataMaxBytes: 512}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
